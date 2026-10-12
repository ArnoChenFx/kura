package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 技能来源类型常量。
const (
	SourceLocal  = "local"
	SourceGitHub = "github"
)

// AddOptions 是 Add 的来源描述：LocalPath 与 Repo 二选一。
type AddOptions struct {
	// LocalPath 是本地技能目录（内含 SKILL.md 的目录本身）。
	LocalPath string
	// Repo 是 GitHub 的 owner/repo。
	Repo string
	// SubPath 是仓库内技能目录（可选）。为空时默认用 Repo 根目录。
	SubPath string
	// Ref 是分支/标签/commit（可选，默认仓库默认分支）。
	Ref string
	// Name 是安装后的技能名（可选）。为空时取来源目录名。
	// 元数据中的来源信息始终记录真实来源，与改名无关。
	Name string
}

// validate 校验来源参数：必须且只能提供一种来源。
func (o *AddOptions) validate() error {
	if (o.LocalPath == "") == (o.Repo == "") {
		return errors.New("store: LocalPath 与 Repo 必须且只能提供一个")
	}
	if o.LocalPath != "" {
		return nil
	}
	if strings.Count(o.Repo, "/") != 1 || strings.ContainsAny(o.Repo, " \t\\") {
		return errors.New("store: Repo 格式应为 owner/repo")
	}
	if strings.HasPrefix(o.SubPath, "/") || strings.Contains(o.SubPath, "..") {
		return errors.New("store: SubPath 不能以 / 开头或包含 ..")
	}
	return nil
}

// Add 把一个技能安装进根目录：从本地目录或 GitHub 仓库。安装时会写入
// .kura-meta.json（记录来源、安装时间与内容哈希）。已存在同名技能时报错。
func (c *Config) Add(opts *AddOptions) (*Result, error) {
	if opts == nil {
		return nil, errors.New("store: AddOptions 不能为空")
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	var srcDir string
	var src Source
	var cleanup func()
	var err error
	if opts.LocalPath != "" {
		srcDir, src, err = c.resolveLocal(opts)
	} else {
		srcDir, src, cleanup, err = c.resolveGitHub(opts)
	}
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	return c.installDir(srcDir, src, opts.Name)
}

// resolveLocal 处理本地目录来源。
func (c *Config) resolveLocal(opts *AddOptions) (string, Source, error) {
	abs, err := filepath.Abs(opts.LocalPath)
	if err != nil {
		return "", Source{}, fmt.Errorf("store: 解析路径失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", Source{}, fmt.Errorf("store: 来源目录不可读: %w", err)
	}
	if !info.IsDir() {
		return "", Source{}, fmt.Errorf("store: 来源不是目录: %s", abs)
	}
	return abs, Source{Type: SourceLocal, Path: abs}, nil
}

// resolveGitHub 用 git clone --depth 1 把 owner/repo 拉到临时目录。
// 返回仓库内技能目录的路径与清理函数（调用方 defer 执行）。
func (c *Config) resolveGitHub(opts *AddOptions) (string, Source, func(), error) {
	tmp, err := os.MkdirTemp("", "kura-clone-")
	if err != nil {
		return "", Source{}, nil, fmt.Errorf("store: 创建临时目录失败: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmp) }
	url := fmt.Sprintf("https://github.com/%s.git", opts.Repo)
	args := []string{"clone", "--depth", "1"}
	if opts.Ref != "" {
		args = append(args, "--branch", opts.Ref)
	}
	args = append(args, url, tmp)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", Source{}, cleanup, fmt.Errorf("store: git clone 失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	dir := tmp
	if opts.SubPath != "" {
		dir = filepath.Join(tmp, filepath.FromSlash(opts.SubPath))
	}
	if !fileExists(dir) {
		return "", Source{}, cleanup, fmt.Errorf("store: 仓库内不存在目录 %s", opts.SubPath)
	}
	// 记录 clone 到的 commit，便于 Update 判断是否有新版本。
	commit := gitHead(dir)
	return dir, Source{Type: SourceGitHub, URL: url, Ref: opts.Ref, SubPath: opts.SubPath, Commit: commit}, cleanup, nil
}

// gitHead 读取 repo 的 HEAD commit（短哈希）。失败返回空串，不阻塞安装。
func gitHead(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// installDir 把 srcDir 复制为根目录下的技能文件夹并写入元数据。
// 技能名优先用 nameOverride（AddOptions.Name），否则取来源目录名；
// 重名或名字非法时报错。元数据里的来源始终是真实来源（srcDir/src），
// 与改名无关。
func (c *Config) installDir(srcDir string, src Source, nameOverride string) (*Result, error) {
	name := nameOverride
	if name == "" {
		name = filepath.Base(srcDir)
	}
	if name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, `\/`) {
		return nil, fmt.Errorf("store: 无法从来源推导技能名: %s", srcDir)
	}
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: 根目录已存在同名技能 %s", name)
	}
	if !fileExists(filepath.Join(srcDir, "SKILL.md")) {
		return nil, fmt.Errorf("store: 来源目录缺少 SKILL.md: %s", srcDir)
	}
	if err := copyDir(srcDir, dst); err != nil {
		return nil, fmt.Errorf("store: 安装 %s 失败: %w", name, err)
	}
	if src.Type == SourceGitHub {
		// clone 的临时目录里带着 .git，安装进技能根目录会污染。
		os.RemoveAll(filepath.Join(dst, ".git"))
	}
	hash, err := HashDir(dst)
	if err != nil {
		os.RemoveAll(dst)
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	meta := &Meta{
		ID:          name,
		DisplayName: name,
		Source:      src,
		InstalledAt: now,
		UpdatedAt:   now,
		ContentHash: hash,
	}
	if err := writeMeta(dst, meta); err != nil {
		os.RemoveAll(dst)
		return nil, err
	}
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}
