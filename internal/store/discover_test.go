package store

// 技能发现与「无 --sub-path 安装」的测试。全部场景都在 t.TempDir() 里构造
// 本地假仓库（不联网），git 相关路径沿用 store_test.go 的 insteadOf 镜像法。
//
// 三种核心情形（任务书要求逐一覆盖）：
//  1. 唯一技能直接装；
//  2. 多技能列候选（CandidatesError）；
//  3. plugin.json 清单优先于扫描。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepoFile 在 base 下按 slash 相对路径写文件（自动建父目录）。
func writeRepoFile(t *testing.T, base, rel, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repoSkill 在 base 下的 slash 相对目录写一个最小技能（SKILL.md）。
func repoSkill(t *testing.T, base, rel string) {
	t.Helper()
	writeRepoFile(t, base, rel+"/SKILL.md", "---\nname: "+filepath.Base(rel)+"\n---\n\n# 技能\n")
}

// TestDiscoverRepoRootIsSkill 验证规则 a：仓库根有 SKILL.md 时整仓即一个技能。
func TestDiscoverRepoRootIsSkill(t *testing.T) {
	dir := t.TempDir()
	repoSkill(t, dir, ".")
	dr, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if len(dr.Paths) != 1 || dr.Paths[0] != "." || dr.Manifest {
		t.Errorf("应返回根目录且非清单来源，得到 %+v", dr)
	}
}

// TestDiscoverScanMultiLevel 验证规则 b：第 3 层的 skills/<cat>/<name>/SKILL.md
// 都能被发现（mattpocock/skills 的布局），node_modules / .git 等被跳过。
func TestDiscoverScanMultiLevel(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"skills/engineering/tdd",
		"skills/engineering/code-review",
		"skills/productivity/grill-me",
		"skills/misc/one-off",
	} {
		repoSkill(t, dir, p)
	}
	// 干扰项：node_modules 里藏的 SKILL.md 不算；超过深度的目录不钻。
	repoSkill(t, dir, "node_modules/fake/SKILL.md")
	repoSkill(t, dir, "skills/engineering/tdd/docs/deep/nested/very/much/SKILL.md") // 深度 > 6
	dr, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if dr.Manifest {
		t.Error("纯扫描不应标记 Manifest")
	}
	want := []string{
		"skills/engineering/code-review",
		"skills/engineering/tdd",
		"skills/misc/one-off",
		"skills/productivity/grill-me",
	}
	if strings.Join(dr.Paths, "|") != strings.Join(want, "|") {
		t.Errorf("候选应 %v，得到 %v", want, dr.Paths)
	}
}

// TestDiscoverManifestPriority 验证规则 c：plugin.json 清单优先于扫描，
// 且清单里指向不存在目录的条目被忽略、指向 SKILL.md 文件的条目取其目录。
func TestDiscoverManifestPriority(t *testing.T) {
	dir := t.TempDir()
	// 扫描会看到 published 与草稿 draft；清单只声明 published。
	for _, p := range []string{"skills/published/alpha", "skills/draft/beta", "skills/published/gamma"} {
		repoSkill(t, dir, p)
	}
	manifest := `{"name":"x","skills":["./skills/published/alpha","./skills/published/gamma","./skills/missing/ghost"]}`
	writeRepoFile(t, dir, ".claude-plugin/plugin.json", manifest)

	dr, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if !dr.Manifest {
		t.Error("清单存在时应标记 Manifest=true")
	}
	want := []string{"skills/published/alpha", "skills/published/gamma"}
	if strings.Join(dr.Paths, "|") != strings.Join(want, "|") {
		t.Errorf("清单应优先且忽略缺失条目，想要 %v，得到 %v", want, dr.Paths)
	}

	// 清单条目写成 SKILL.md 文件路径：取其所在目录。
	writeRepoFile(t, dir, ".claude-plugin/plugin.json",
		`{"skills":["./skills/published/alpha/SKILL.md"]}`)
	dr, err = DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if strings.Join(dr.Paths, "|") != "skills/published/alpha" {
		t.Errorf("文件条目应取所在目录，得到 %v", dr.Paths)
	}

	// 清单损坏（坏 JSON）→ 回退扫描，草稿也在列。
	writeRepoFile(t, dir, ".claude-plugin/plugin.json", "{not json")
	dr, err = DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if dr.Manifest {
		t.Error("清单损坏应回退扫描")
	}
	if strings.Join(dr.Paths, "|") != "skills/draft/beta|skills/published/alpha|skills/published/gamma" {
		t.Errorf("回退扫描应列出全部，得到 %v", dr.Paths)
	}
}

// TestDiscoverDeepNesting 验证深度上限内的第 6 层能被发现。
func TestDiscoverDeepNesting(t *testing.T) {
	dir := t.TempDir()
	deep := "a/b/c/d/e/skill-here" // 6 层
	repoSkill(t, dir, deep)
	dr, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("DiscoverSkills 失败: %v", err)
	}
	if strings.Join(dr.Paths, "|") != deep {
		t.Errorf("第 6 层技能应被发现，得到 %v", dr.Paths)
	}
}

// TestDiscoverTooManySkills 验证候选超过上限时报 ErrTooManySkills。
func TestDiscoverTooManySkills(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= maxDiscoverSkills; i++ {
		repoSkill(t, dir, fmt.Sprintf("pool/s%02d", i))
	}
	_, err := DiscoverSkills(dir)
	if !errors.Is(err, ErrTooManySkills) {
		t.Fatalf("应返回 ErrTooManySkills，得到 %v", err)
	}
}

// TestDiscoverMissingDir 验证目录不存在时报错（不 panic）。
func TestDiscoverMissingDir(t *testing.T) {
	if _, err := DiscoverSkills(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("目录不存在应报错")
	}
}

// TestCandidatesErrorMessage 验证多候选错误信息可读、包含 repo 与路径。
func TestCandidatesErrorMessage(t *testing.T) {
	e := &CandidatesError{Repo: "mattpocock/skills", Paths: []string{"skills/a/x", "skills/b/y"}}
	msg := e.Error()
	if !strings.Contains(msg, "mattpocock/skills") || !strings.Contains(msg, "2 个技能") ||
		!strings.Contains(msg, "skills/a/x") || !strings.Contains(msg, "--sub-path") {
		t.Errorf("错误信息应列出候选并提示 --sub-path：%s", msg)
	}
	// 用 errors.As 断言类型可在调用方识别（CLI/UI 的契约）。
	var ce *CandidatesError
	if !errors.As(error(e), &ce) || len(ce.Paths) != 2 {
		t.Error("CandidatesError 应能用 errors.As 还原")
	}
}

// installMirror 把本地 repo 镜像成 bare 并用 insteadOf 重写指定 URL（同
// TestAddGitHub 的做法）；返回 restore 供 t.Cleanup 注销 insteadOf。
func installMirror(t *testing.T, repo, alias string) func() {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bare := filepath.Join(t.TempDir(), "bare.git")
	run("clone", "-q", "--bare", repo, bare)
	key := "url." + bare + ".insteadOf"
	run("config", "--global", key, "https://github.com/"+alias+".git")
	return func() { exec.Command("git", "config", "--global", "--unset", key).Run() } //nolint:errcheck
}

// commitRepo 把 repo 里的一切提交一次（带隔离的 user 配置）。
func commitRepo(t *testing.T, repo string) {
	t.Helper()
	gitCLI := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitCLI("init", "-q")
	gitCLI("config", "user.email", "t@t")
	gitCLI("config", "user.name", "t")
	gitCLI("add", ".")
	gitCLI("commit", "-q", "-m", "init")
}

// TestAddGitHubAutoDiscoverSingle 验证情形 1：仓库里恰好一个技能时直接装。
func TestAddGitHubAutoDiscoverSingle(t *testing.T) {
	repo := t.TempDir()
	repoSkill(t, repo, "skills/only/lonely")
	commitRepo(t, repo)
	c := newTestConfig(t)
	restore := installMirror(t, repo, "owner/single")
	t.Cleanup(restore)

	r, err := c.Add(&AddOptions{Repo: "owner/single"})
	if err != nil {
		t.Fatalf("自动发现单技能应直接安装: %v", err)
	}
	if r.ID != "lonely" {
		t.Errorf("技能名应取技能目录名，得到 %q", r.ID)
	}
	m := readMeta(c.SkillPath("lonely")).Meta
	if m == nil || m.Source.SubPath != "skills/only/lonely" {
		t.Errorf("元数据应记录发现的 SubPath，得到 %+v", m)
	}
}

// TestAddGitHubRepoRootSkill 验证规则 a 的安装路径：仓库根是技能时按根装，
// 名字默认取仓库名（不是随机的临时目录名），SubPath 为空。
func TestAddGitHubRepoRootSkill(t *testing.T) {
	repo := t.TempDir()
	repoSkill(t, repo, ".")
	commitRepo(t, repo)
	c := newTestConfig(t)
	restore := installMirror(t, repo, "owner/rootskill")
	t.Cleanup(restore)

	r, err := c.Add(&AddOptions{Repo: "owner/rootskill"})
	if err != nil {
		t.Fatalf("仓库根技能应直接安装: %v", err)
	}
	if r.ID != "rootskill" {
		t.Errorf("默认名应取仓库名 rootskill，得到 %q", r.ID)
	}
	m := readMeta(c.SkillPath("rootskill")).Meta
	if m == nil || m.Source.SubPath != "" {
		t.Errorf("根目录安装的 SubPath 应为空，得到 %+v", m)
	}
}

// TestAddGitHubAutoDiscoverMulti 验证情形 2：多个候选返回 CandidatesError，
// 且显式 --sub-path 仍然照装（向后兼容）。
func TestAddGitHubAutoDiscoverMulti(t *testing.T) {
	repo := t.TempDir()
	repoSkill(t, repo, "skills/engineering/tdd")
	repoSkill(t, repo, "skills/engineering/code-review")
	repoSkill(t, repo, "skills/productivity/grill-me")
	commitRepo(t, repo)
	c := newTestConfig(t)
	restore := installMirror(t, repo, "owner/multi")
	t.Cleanup(restore)

	_, err := c.Add(&AddOptions{Repo: "owner/multi"})
	var ce *CandidatesError
	if !errors.As(err, &ce) {
		t.Fatalf("多候选应返回 CandidatesError，得到 %v", err)
	}
	if len(ce.Paths) != 3 {
		t.Errorf("应列出 3 个候选，得到 %v", ce.Paths)
	}

	// 显式 --sub-path：一律照用户指定的装。
	r, err := c.Add(&AddOptions{Repo: "owner/multi", SubPath: "skills/engineering/tdd"})
	if err != nil {
		t.Fatalf("显式 --sub-path 应照装: %v", err)
	}
	if r.ID != "tdd" {
		t.Errorf("技能名应取 SubPath 尾段，得到 %q", r.ID)
	}
}

// TestAddGitHubAutoDiscoverNone 验证情形 0：仓库里没有任何技能时沿用
// 旧报错文案「来源目录缺少 SKILL.md」（与显式 --sub-path 指错目录一致）。
func TestAddGitHubAutoDiscoverNone(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "README.md", "只有文档，没有技能\n")
	commitRepo(t, repo)
	c := newTestConfig(t)
	restore := installMirror(t, repo, "owner/none")
	t.Cleanup(restore)

	_, err := c.Add(&AddOptions{Repo: "owner/none"})
	if err == nil || !strings.Contains(err.Error(), "来源目录缺少 SKILL.md") {
		t.Fatalf("零候选应报「来源目录缺少 SKILL.md」，得到 %v", err)
	}
}
