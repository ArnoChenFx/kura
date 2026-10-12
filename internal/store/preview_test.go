package store

// PreviewUpdate 的测试（里程碑 4）。全部场景都在 t.TempDir() 里用本地 git
// 仓库构造（无网络）；绝不触碰真实的 ~/.agents/skills，也不写用户的真实
// git 全局配置（isolatedGitEnv 把全局/系统配置重定向到空临时文件）。
//
// 场景覆盖：changed / added / removed / drifted / unchanged，外加：
//   - 预览不改盘上内容（技能目录与 snapshots/ 都不动）；
//   - 元数据缺失/损坏、来源缺失等错误路径；
//   - NewHash 与真实 Update 完成后的内容哈希一致（跨函数契约）。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedGitEnv 把子进程 git 的 global/system 配置重定向到空临时文件：
//   - 屏蔽用户机器上的 core.autocrlf（本机为 true 时 clone 会把 LF 转成
//     CRLF，导致预览的行级对比在 Windows 上全行不匹配）；
//   - mirror 的 insteadOf 只落在临时文件里，不污染真实全局配置。
//   - 环境变量由 t.Setenv 自动还原。
func isolatedGitEnv(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// gitMirror 把一个本地仓库镜像为 bare，并用 insteadOf 让
// https://github.com/owner/<alias>.git 的 clone 实际落在本地（无网络）。
// sync 可在上游推进后重新镜像；同一时刻保证只有一个 insteadOf 规则生效
// （多个规则匹配同一 URL 时 git 的选择不可靠，见 ChangedAddedRemoved 的教训）。
type gitMirror struct {
	t     *testing.T
	alias string
	key   string // 当前生效的 insteadOf 配置键
}

// newMirror 建立镜像并注册清理（测试结束时自动 unset insteadOf）。
func newMirror(t *testing.T, repo, alias string) *gitMirror {
	t.Helper()
	m := &gitMirror{t: t, alias: alias}
	m.sync(repo)
	t.Cleanup(m.unset)
	return m
}

// sync 重新镜像 repo 到新 bare，并把 insteadOf 指过去（先清旧键）。
func (m *gitMirror) sync(repo string) {
	m.unset()
	bare := filepath.Join(m.t.TempDir(), "bare.git")
	gitCmd(m.t, "", "clone", "-q", "--bare", repo, bare)
	m.key = "url." + bare + ".insteadOf"
	gitCmd(m.t, "", "config", "--global", m.key, "https://github.com/owner/"+m.alias+".git")
}

// unset 清掉当前 insteadOf 键（键不存在时 git 返回非零，忽略即可）。
func (m *gitMirror) unset() {
	if m.key != "" {
		exec.Command("git", "config", "--global", "--unset", m.key).Run() //nolint:errcheck
		m.key = ""
	}
}

// gitCmd 在 dir 下执行 git 命令，失败即 Fatal（dir 为空时在当前目录执行）。
func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// previewRepo 在 base 下建一个 git 仓库（工作树 + .git），技能目录为
// skills/<name>，内容由 files 表驱动（相对技能目录的路径 → 文件内容），
// 提交一次后返回仓库路径。
func previewRepo(t *testing.T, base, name string, files map[string]string) string {
	t.Helper()
	repo := filepath.Join(base, "repo")
	dir := filepath.Join(repo, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, repo, "init", "-q")
	gitCmd(t, repo, "config", "user.email", "t@t")
	gitCmd(t, repo, "config", "user.name", "t")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// installFromGitHub 用 kura.Add 把 owner/<alias> 的 skills/<name> 安装进
// root，返回安装时记录的 commit。
func installFromGitHub(t *testing.T, c *Config, alias, name string) string {
	t.Helper()
	if _, err := c.Add(&AddOptions{Repo: "owner/" + alias, SubPath: "skills/" + name}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	m := readMeta(c.SkillPath(name)).Meta
	if m == nil {
		t.Fatal("安装后应有元数据")
	}
	return m.Source.Commit
}

// advanceUpstream 修改 repo 内 skills/<name> 的文件并提交（表驱动：
// 写入 files、删除 dels 里的相对路径），返回新 commit。
func advanceUpstream(t *testing.T, repo, name string, files map[string]string, dels ...string) string {
	t.Helper()
	dir := filepath.Join(repo, "skills", name)
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range dels {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "upstream change")
	return previewGitOut(t, repo, "rev-parse", "HEAD")
}

// previewGitOut 执行 git 命令并返回 stdout（去掉首尾空白）。
func previewGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// findDiff 按 path 在 Files 里找 FileDiff。
func findDiff(p *UpdatePreview, path string) (FileDiff, bool) {
	for _, f := range p.Files {
		if f.Path == path {
			return f, true
		}
	}
	return FileDiff{}, false
}

// ---- 场景一：changed + added + removed 一次覆盖 ----

func TestPreviewUpdateChangedAddedRemoved(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	base := t.TempDir()
	// v1：SKILL.md + old.md + keep.txt。
	repo := previewRepo(t, base, "demo", map[string]string{
		"SKILL.md": "---\nname: demo\n---\n\nline1\nline2\nline3\nline4\nline5\n",
		"old.md":   "old line 1\nold line 2\nold line 3\n",
		"keep.txt": "same\n",
	})
	mirror := newMirror(t, repo, "repo1")

	if got := installFromGitHub(t, c, "repo1", "demo"); got == "" {
		t.Fatal("安装应记录 commit")
	}

	// 上游推进到 v2：改 SKILL.md、删 old.md、加 new.md，并重新镜像。
	commit := advanceUpstream(t, repo, "demo", map[string]string{
		"SKILL.md": "---\nname: demo\n---\n\nline1\nline2\nline3-changed\nline4\nline5\nline6-new\n",
		"new.md":   "fresh 1\nfresh 2\n",
	}, "old.md")
	mirror.sync(repo)

	p, err := c.PreviewUpdate("demo")
	if err != nil {
		t.Fatalf("PreviewUpdate 失败: %v", err)
	}
	if p.ID != "demo" || p.SourceType != SourceGitHub {
		t.Errorf("ID/SourceType 不符: %+v", p)
	}
	if p.Commit != commit {
		t.Errorf("上游 commit 应为 %s, got %q", commit, p.Commit)
	}
	added, changed, removed := p.Counts()
	if added != 1 || changed != 1 || removed != 1 {
		t.Fatalf("Counts 应为 1/1/1, got %d/%d/%d (%+v)", added, changed, removed, p.Files)
	}
	if !p.HasChanges() {
		t.Error("HasChanges 应为 true")
	}
	// SKILL.md：第 3 行被改、末尾加 1 行 → +2/-1。
	fd, ok := findDiff(p, "SKILL.md")
	if !ok || fd.Status != DiffChanged {
		t.Fatalf("SKILL.md 应是 changed, got %+v (files=%v)", fd, p.Files)
	}
	if fd.Added != 2 || fd.Removed != 1 {
		t.Errorf("SKILL.md 行数应为 +2/-1, got +%d/-%d", fd.Added, fd.Removed)
	}
	if fd.OldHash == "" || fd.NewHash == "" || fd.OldHash == fd.NewHash {
		t.Errorf("SKILL.md 新旧哈希应不同且非空: %q vs %q", fd.OldHash, fd.NewHash)
	}
	// new.md：added；old.md：removed。
	if fd, ok := findDiff(p, "new.md"); !ok || fd.Status != DiffAdded || fd.Added != 2 {
		t.Errorf("new.md 应为 added(+2): %+v", fd)
	}
	if fd, ok := findDiff(p, "old.md"); !ok || fd.Status != DiffRemoved || fd.Removed != 3 {
		t.Errorf("old.md 应为 removed(-3): %+v", fd)
	}
	// keep.txt 未变，不计入 Files。
	if _, ok := findDiff(p, "keep.txt"); ok {
		t.Error("未变化的 keep.txt 不应出现在 Files")
	}
	if p.Unchanged != 1 {
		t.Errorf("Unchanged 应为 1, got %d", p.Unchanged)
	}
	// Files 按路径排序。
	for i := 1; i < len(p.Files); i++ {
		if p.Files[i-1].Path > p.Files[i].Path {
			t.Errorf("Files 应按路径排序: %v", p.Files)
			break
		}
	}
}

// ---- 场景二：无任何变化 ----

func TestPreviewUpdateNoChanges(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := previewRepo(t, t.TempDir(), "same", map[string]string{
		"SKILL.md": "---\nname: same\n---\n\nsame\n",
	})
	newMirror(t, repo, "repo2")
	installFromGitHub(t, c, "repo2", "same")

	p, err := c.PreviewUpdate("same")
	if err != nil {
		t.Fatalf("PreviewUpdate 失败: %v", err)
	}
	if p.HasChanges() || len(p.Files) != 0 {
		t.Errorf("无变化时 Files 应为空: %+v", p.Files)
	}
	if p.Unchanged != 1 {
		t.Errorf("Unchanged 应为 1, got %d", p.Unchanged)
	}
	added, changed, removed := p.Counts()
	if added != 0 || changed != 0 || removed != 0 {
		t.Errorf("Counts 应全为 0, got %d/%d/%d", added, changed, removed)
	}
}

// ---- 场景三：内容漂移（本地被手改，上游不动） ----

func TestPreviewUpdateDrifted(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := previewRepo(t, t.TempDir(), "drift", map[string]string{
		"SKILL.md": "---\nname: drift\n---\n\nupstream\n",
		"keep.txt": "same\n",
	})
	newMirror(t, repo, "repo3")
	installFromGitHub(t, c, "repo3", "drift")

	// 手工改动已安装的内容（不推进上游）→ 预览应把本地漂移报告为 changed。
	driftDir := c.SkillPath("drift")
	if err := os.WriteFile(filepath.Join(driftDir, "SKILL.md"),
		[]byte("---\nname: drift\n---\n\nlocally edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := c.PreviewUpdate("drift")
	if err != nil {
		t.Fatalf("PreviewUpdate 失败: %v", err)
	}
	fd, ok := findDiff(p, "SKILL.md")
	if !ok || fd.Status != DiffChanged || fd.Added != 1 || fd.Removed != 1 {
		t.Fatalf("漂移文件应为 changed(+1/-1): %+v (files=%v)", fd, p.Files)
	}
	// keep.txt 未漂移 → Unchanged=1，Files 只有 SKILL.md。
	if p.Unchanged != 1 || len(p.Files) != 1 {
		t.Errorf("Unchanged/Files 应为 1/1, got %d/%d", p.Unchanged, len(p.Files))
	}
	// 元数据里记录的 contentHash 与当前内容不一致（漂移成立）。
	m := readMeta(driftDir).Meta
	cur, err := HashDir(driftDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ContentHash == cur {
		t.Error("手改后元数据哈希应与当前内容不一致（漂移成立）")
	}
}

// ---- 场景四：预览绝不修改盘上内容 ----

func TestPreviewUpdateDoesNotTouchDisk(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := previewRepo(t, t.TempDir(), "frozen", map[string]string{
		"SKILL.md": "---\nname: frozen\n---\n\nv1\n",
	})
	mirror := newMirror(t, repo, "repo4")
	installFromGitHub(t, c, "repo4", "frozen")

	// 上游推进 v2（有真实差异可预览）。
	advanceUpstream(t, repo, "frozen", map[string]string{
		"SKILL.md": "---\nname: frozen\n---\n\nv2\n",
	})
	mirror.sync(repo)

	// 记录预览前的全部状态。
	rootBefore := diskFingerprint(t, c.Root)
	snapBefore := diskFingerprint(t, c.SnapshotsRoot()) // 可能不存在

	p, err := c.PreviewUpdate("frozen")
	if err != nil {
		t.Fatalf("PreviewUpdate 失败: %v", err)
	}
	if !p.HasChanges() {
		t.Fatal("v1→v2 预览应发现变化（否则本测试没有检验力）")
	}
	// 根目录与快照目录必须与预览前逐字节一致。
	if after := diskFingerprint(t, c.Root); after != rootBefore {
		t.Error("预览不得修改技能根目录")
	}
	if after := diskFingerprint(t, c.SnapshotsRoot()); after != snapBefore {
		t.Error("预览不得创建快照")
	}
	// 元数据也未被改写（updatedAt/commit 原样）。
	m := readMeta(c.SkillPath("frozen")).Meta
	if m == nil || m.Source.Commit == "" || m.UpdatedAt == "" {
		t.Errorf("元数据应保持原样: %+v", m)
	}
}

// diskFingerprint 对 dir 做稳定指纹（相对路径+内容），目录不存在返回 "absent"。
func diskFingerprint(t *testing.T, dir string) string {
	t.Helper()
	if !fileExists(dir) {
		return "absent"
	}
	h, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// ---- 场景五：NewHash 与真实 Update 后的内容哈希一致（跨函数契约） ----

func TestPreviewUpdateNewHashMatchesUpdate(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := previewRepo(t, t.TempDir(), "sync", map[string]string{
		"SKILL.md": "---\nname: sync\n---\n\nv1\n",
	})
	mirror := newMirror(t, repo, "repo5")
	installFromGitHub(t, c, "repo5", "sync")

	// 上游推进 v2 并重新镜像。
	advanceUpstream(t, repo, "sync", map[string]string{
		"SKILL.md": "---\nname: sync\n---\n\nv2\n",
	})
	mirror.sync(repo)

	p, err := c.PreviewUpdate("sync")
	if err != nil {
		t.Fatalf("PreviewUpdate 失败: %v", err)
	}
	if !p.HasChanges() {
		t.Fatal("预览应发现 v1→v2 变化（否则本测试没有检验力）")
	}
	if _, err := c.Update("sync"); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	got, err := HashDir(c.SkillPath("sync"))
	if err != nil {
		t.Fatal(err)
	}
	if p.NewHash != got {
		t.Errorf("预览 NewHash %q 应等于更新后的内容哈希 %q", p.NewHash, got)
	}
}

// ---- 场景六：local 来源 ----

func TestPreviewUpdateLocalSource(t *testing.T) {
	c := newTestConfig(t)
	// 本地来源目录：与已安装内容有一个文件不同。
	src := filepath.Join(t.TempDir(), "src-skill")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: src\n---\n\nv1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(&AddOptions{LocalPath: src}); err != nil {
		t.Fatalf("Add(local) 失败: %v", err)
	}
	// 来源推进：加一个文件、改 SKILL.md。
	if err := os.WriteFile(filepath.Join(src, "extra.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: src\n---\n\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := c.PreviewUpdate("src-skill")
	if err != nil {
		t.Fatalf("PreviewUpdate(local) 失败: %v", err)
	}
	if p.SourceType != SourceLocal || p.Commit != "" {
		t.Errorf("local 来源: SourceType=%q Commit=%q", p.SourceType, p.Commit)
	}
	if _, ok := findDiff(p, "extra.md"); !ok {
		t.Errorf("extra.md 应为 added: %+v", p.Files)
	}
	if fd, ok := findDiff(p, "SKILL.md"); !ok || fd.Added != 1 || fd.Removed != 1 {
		t.Errorf("SKILL.md 应为 changed(+1/-1): %+v", fd)
	}
}

// ---- 场景七：错误路径 ----

func TestPreviewUpdateErrors(t *testing.T) {
	c := newTestConfig(t)
	// 未知技能。
	if _, err := c.PreviewUpdate("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("未知技能应返回 ErrUnknownSkill, got %v", err)
	}
	// 缺元数据。
	writeSkill(t, c.Root, "nometa", nil)
	if _, err := c.PreviewUpdate("nometa"); err == nil || !strings.Contains(err.Error(), "缺少 .kura-meta.json") {
		t.Errorf("缺元数据应报错, got %v", err)
	}
	// 元数据损坏。
	bad := c.SkillPath("badmeta")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, MetaFileName), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PreviewUpdate("badmeta"); err == nil || !strings.Contains(err.Error(), "不可读") {
		t.Errorf("损坏元数据应报错, got %v", err)
	}
	// local 来源路径消失。
	gone := filepath.Join(t.TempDir(), "gone-src")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gone, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(&AddOptions{LocalPath: gone}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PreviewUpdate("gone-src"); err == nil || !strings.Contains(err.Error(), "本地来源不存在") {
		t.Errorf("来源消失应报错, got %v", err)
	}
}
