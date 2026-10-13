package store

// 轻量检查更新与同仓库复用 clone 的测试。全部在 t.TempDir() 里用本地 git
// 仓库构造（经 gitMirror 的 insteadOf 冒充 github 地址），无网络。

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// countClones 统计测试期间 cloneUpstream 的调用次数。
func countClones(t *testing.T) *int {
	t.Helper()
	n := 0
	cloneHook = func(string) { n++ }
	t.Cleanup(func() { cloneHook = nil })
	return &n
}

// twoSkillRepo 建一个含 skills/a、skills/b 与一个无关大文件的仓库，
// 镜像为 github.com/owner/<alias>，并把两个技能都装进 c。
func twoSkillRepo(t *testing.T, c *Config, alias string) (string, *gitMirror) {
	t.Helper()
	repo := previewRepo(t, t.TempDir(), "a", map[string]string{
		"SKILL.md": "---\nname: a\n---\n\na v1\n",
	})
	writeRepoFile(t, repo, "skills/b/SKILL.md", "---\nname: b\n---\n\nb v1\n")
	writeRepoFile(t, repo, "assets/unrelated.bin", "large unrelated content\n")
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "add b")
	m := newMirror(t, repo, alias)
	installFromGitHub(t, c, alias, "a")
	installFromGitHub(t, c, alias, "b")
	return repo, m
}

// writeRepoFile 在仓库工作树里写一个文件（rel 为 / 分隔）。
func writeRepoFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 安装记录的 commit 与树哈希就是上游的值，且只检出技能子目录。
func TestAddRecordsCommitAndTree(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, _ := twoSkillRepo(t, c, "tree1")
	m := readMeta(c.SkillPath("a")).Meta
	if want := previewGitOut(t, repo, "rev-parse", "HEAD"); m.Source.Commit != want {
		t.Errorf("commit 应为 %s, got %q", want, m.Source.Commit)
	}
	if want := previewGitOut(t, repo, "rev-parse", "HEAD:skills/a"); m.Source.Tree != want {
		t.Errorf("tree 应为 %s, got %q", want, m.Source.Tree)
	}
}

// 稀疏检出只把要的子目录放进工作区。
func TestUpstreamCheckoutIsSparse(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	twoSkillRepo(t, c, "sparse1")
	u, err := cloneUpstream("https://github.com/owner/sparse1.git", "")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if err := u.Checkout([]string{"skills/a"}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(u.Dir("skills/a"), "SKILL.md")) {
		t.Error("skills/a 应被检出")
	}
	for _, rel := range []string{"skills/b", "assets"} {
		if fileExists(filepath.Join(u.dir, filepath.FromSlash(rel))) {
			t.Errorf("%s 不应被检出", rel)
		}
	}
	if _, err := u.Tree("skills/missing"); err == nil {
		t.Error("不存在的子目录应报错")
	}
}

// 上游只改了 a：同仓库的两个技能只 clone 一次；a 逐文件比较，b 凭树哈希判定最新。
func TestPreviewUpdatesSharesCloneAndUsesTree(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, mirror := twoSkillRepo(t, c, "share1")
	advanceUpstream(t, repo, "a", map[string]string{"SKILL.md": "---\nname: a\n---\n\na v2\n"})
	mirror.sync(repo)

	clones := countClones(t)
	rs := c.PreviewUpdates([]string{"a", "b", "ghost"})
	if *clones != 1 {
		t.Errorf("同仓库应只 clone 一次, got %d", *clones)
	}
	if rs[0].Err != nil || rs[1].Err != nil {
		t.Fatalf("预览失败: %v / %v", rs[0].Err, rs[1].Err)
	}
	if a := rs[0].Preview; !a.HasChanges() || a.Basis != BasisContent {
		t.Errorf("a 应有更新且逐文件比较: basis=%q files=%v", a.Basis, a.Files)
	}
	if b := rs[1].Preview; b.HasChanges() || b.Basis != BasisTree {
		t.Errorf("b 应凭树哈希判定最新: basis=%q files=%v", b.Basis, b.Files)
	}
	if !errors.Is(rs[2].Err, ErrUnknownSkill) {
		t.Errorf("ghost 应报 ErrUnknownSkill, got %v", rs[2].Err)
	}
}

// 上游没动：只 ls-remote，不 clone。
func TestPreviewUpdatesNoCloneWhenCommitSame(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	twoSkillRepo(t, c, "same1")
	clones := countClones(t)
	for _, r := range c.PreviewUpdates([]string{"a", "b"}) {
		if r.Err != nil || r.Preview.HasChanges() || r.Preview.Basis != BasisCommit {
			t.Errorf("%s 应凭 commit 判定最新: %+v %v", r.Name, r.Preview, r.Err)
		}
	}
	if *clones != 0 {
		t.Errorf("commit 未变时不应 clone, got %d", *clones)
	}
}

// 旧元数据没有树哈希（导入的或旧版本安装的）：退回逐文件比较。
func TestPreviewUpdateLegacyMetaFallsBackToContent(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, mirror := twoSkillRepo(t, c, "legacy1")
	dir := c.SkillPath("b")
	m := readMeta(dir).Meta
	m.Source.Commit, m.Source.Tree = "", ""
	if err := writeMeta(dir, m); err != nil {
		t.Fatal(err)
	}
	advanceUpstream(t, repo, "a", map[string]string{"SKILL.md": "a v2\n"})
	mirror.sync(repo)
	p, err := c.PreviewUpdate("b")
	if err != nil {
		t.Fatal(err)
	}
	if p.Basis != BasisContent || p.HasChanges() || p.Unchanged != 1 {
		t.Errorf("应逐文件比较且无差异: basis=%q files=%v unchanged=%d", p.Basis, p.Files, p.Unchanged)
	}
}

// 同仓库的多个技能一起更新：只 clone 一次，元数据记下上游的 commit 与树哈希。
func TestUpdateManySharesCloneAndRecordsCommit(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, mirror := twoSkillRepo(t, c, "upd1")
	advanceUpstream(t, repo, "a", map[string]string{"SKILL.md": "---\nname: a\n---\n\na v2\n"})
	writeRepoFile(t, repo, "skills/b/extra.md", "b extra\n")
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "b extra")
	mirror.sync(repo)
	head := previewGitOut(t, repo, "rev-parse", "HEAD")

	clones := countClones(t)
	for _, r := range c.UpdateMany([]string{"a", "b"}) {
		if r.Err != nil {
			t.Fatalf("更新 %s 失败: %v", r.Name, r.Err)
		}
	}
	if *clones != 1 {
		t.Errorf("同仓库应只 clone 一次, got %d", *clones)
	}
	for _, name := range []string{"a", "b"} {
		m := readMeta(c.SkillPath(name)).Meta
		if m.Source.Commit != head {
			t.Errorf("%s 的 commit 应为上游 HEAD %s, got %q", name, head, m.Source.Commit)
		}
		if want := previewGitOut(t, repo, "rev-parse", "HEAD:skills/"+name); m.Source.Tree != want {
			t.Errorf("%s 的 tree 应为 %s, got %q", name, want, m.Source.Tree)
		}
		if fileExists(filepath.Join(c.SkillPath(name), ".git")) {
			t.Errorf("%s 里不应带 .git", name)
		}
	}
	if !fileExists(filepath.Join(c.SkillPath("b"), "extra.md")) {
		t.Error("b 应拿到上游新增的 extra.md")
	}
	// 更新后再检查：commit 一致，直接判定最新。
	p, err := c.PreviewUpdate("a")
	if err != nil || p.HasChanges() || p.Basis != BasisCommit {
		t.Errorf("更新后应凭 commit 判定最新: %+v %v", p, err)
	}
}

// git 命令超时返回 ErrGitTimeout，而不是无限等待。
func TestRunGitTimeout(t *testing.T) {
	if _, err := runGit(time.Nanosecond, "", "--version"); !errors.Is(err, ErrGitTimeout) {
		t.Errorf("超时应返回 ErrGitTimeout, got %v", err)
	}
}

// ls-remote 取 ref：附注标签要取剥离后的 commit，不存在的 ref 报错。
func TestRemoteHeadRefs(t *testing.T) {
	isolatedGitEnv(t)
	repo := previewRepo(t, t.TempDir(), "x", map[string]string{"SKILL.md": "x\n"})
	gitCmd(t, repo, "tag", "-a", "v1", "-m", "v1")
	gitCmd(t, repo, "branch", "dev")
	newMirror(t, repo, "refs1")
	url := "https://github.com/owner/refs1.git"
	head := previewGitOut(t, repo, "rev-parse", "HEAD")
	for _, ref := range []string{"", "v1", "dev"} {
		if got, err := remoteHead(url, ref); err != nil || got != head {
			t.Errorf("remoteHead(%q) = %q, %v; want %s", ref, got, err, head)
		}
	}
	if _, err := remoteHead(url, "nope"); err == nil {
		t.Error("不存在的 ref 应报错")
	}
}

// 部分 clone：只取树，技能无关的大文件不会被下载（通过 file:// 协议走真正的
// 部分 clone；本地路径 clone 会忽略 --filter，所以这里不能用 gitMirror）。
func TestUpstreamPartialCloneSkipsUnrelatedBlobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file:// URL 的路径写法因平台而异，只在 Unix 上验证")
	}
	isolatedGitEnv(t)
	repo := previewRepo(t, t.TempDir(), "a", map[string]string{"SKILL.md": "a\n"})
	// 与技能无关的大文件：随机内容，压缩不掉。
	big := make([]byte, 2<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "assets/big.bin", string(big))
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "add big")
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitCmd(t, "", "clone", "-q", "--bare", repo, bare)
	gitCmd(t, bare, "config", "uploadpack.allowFilter", "true")
	key := "url.file://" + bare + ".insteadOf"
	gitCmd(t, "", "config", "--global", key, "https://github.com/owner/filt.git")
	t.Cleanup(func() { gitCmd(t, "", "config", "--global", "--unset", key) })

	u, err := cloneUpstream("https://github.com/owner/filt.git", "")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if tree, err := u.Tree("skills/a"); err != nil || tree == "" {
		t.Fatalf("应能取到技能目录的树哈希: %q %v", tree, err)
	}
	if size := dirSize(t, filepath.Join(u.dir, ".git")); size > 512<<10 {
		t.Errorf("只取树时不应下载大文件，.git 有 %d 字节", size)
	}
	if err := u.Checkout([]string{"skills/a"}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(u.Dir("skills/a"), "SKILL.md")) {
		t.Error("skills/a 应被检出")
	}
	if size := dirSize(t, filepath.Join(u.dir, ".git")); size > 512<<10 {
		t.Errorf("稀疏检出后仍不应下载无关的大文件，.git 有 %d 字节", size)
	}
}

// dirSize 返回目录下全部文件的字节数之和。
func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			n += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// 同仓库里某个技能的目录在上游被删了：只有它自己失败，其他技能照常检查、更新。
func TestMissingSubPathOnlyFailsThatSkill(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, mirror := twoSkillRepo(t, c, "gone1")
	advanceUpstream(t, repo, "a", map[string]string{"SKILL.md": "---\nname: a\n---\n\na v2\n"})
	if err := os.RemoveAll(filepath.Join(repo, "skills", "b")); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "drop b")
	mirror.sync(repo)

	pr := c.PreviewUpdates([]string{"a", "b"})
	if pr[0].Err != nil || !pr[0].Preview.HasChanges() {
		t.Errorf("a 应有更新: %+v %v", pr[0].Preview, pr[0].Err)
	}
	if pr[1].Err == nil || !strings.Contains(pr[1].Err.Error(), "仓库内不存在目录 skills/b") {
		t.Errorf("b 应报目录不存在, got %v", pr[1].Err)
	}
	ur := c.UpdateMany([]string{"a", "b"})
	if ur[0].Err != nil {
		t.Errorf("a 应更新成功: %v", ur[0].Err)
	}
	if ur[1].Err == nil || !fileExists(filepath.Join(c.SkillPath("b"), "SKILL.md")) {
		t.Errorf("b 应失败且保持原样: %v", ur[1].Err)
	}
}

// 技能就是仓库根（没有 subPath）：整个仓库检出，.git 不会进技能目录，
// 记录的树哈希是仓库根的树。
func TestRootSkillRoundTrip(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := filepath.Join(t.TempDir(), "repo")
	writeRepoFile(t, repo, "SKILL.md", "---\nname: rootskill\n---\n\nv1\n")
	gitCmd(t, repo, "init", "-q")
	gitCmd(t, repo, "config", "user.email", "t@t")
	gitCmd(t, repo, "config", "user.name", "t")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-q", "-m", "init")
	mirror := newMirror(t, repo, "rootskill")

	if _, err := c.Add(&AddOptions{Repo: "owner/rootskill", Name: "rootskill"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	m := readMeta(c.SkillPath("rootskill")).Meta
	if want := previewGitOut(t, repo, "rev-parse", "HEAD^{tree}"); m.Source.Tree != want {
		t.Errorf("tree 应为仓库根的树 %s, got %q", want, m.Source.Tree)
	}
	writeRepoFile(t, repo, "SKILL.md", "---\nname: rootskill\n---\n\nv2\n")
	gitCmd(t, repo, "commit", "-qam", "v2")
	mirror.sync(repo)

	p, err := c.PreviewUpdate("rootskill")
	if err != nil || !p.HasChanges() || p.Basis != BasisContent {
		t.Fatalf("应逐文件发现 v2: %+v %v", p, err)
	}
	if r := c.UpdateMany([]string{"rootskill"})[0]; r.Err != nil {
		t.Fatalf("更新失败: %v", r.Err)
	}
	if fileExists(filepath.Join(c.SkillPath("rootskill"), ".git")) {
		t.Error("技能目录里不应带 .git")
	}
	got, _ := os.ReadFile(filepath.Join(c.SkillPath("rootskill"), "SKILL.md"))
	if !strings.Contains(string(got), "v2") {
		t.Errorf("应更新到 v2, got %q", got)
	}
}

// 同一个仓库写成带不带 .git、末尾斜杠，都算同一个 key。
func TestRepoKeyNormalizesURL(t *testing.T) {
	a := newRepoKey("https://github.com/o/r.git", "main")
	for _, u := range []string{"https://github.com/o/r", "https://github.com/o/r/", "https://github.com/o/r.git/"} {
		if b := newRepoKey(u, "main"); a != b {
			t.Errorf("%q 应与带 .git 的写法同 key: %+v vs %+v", u, b, a)
		}
	}
	if newRepoKey("https://github.com/o/r.git", "dev") == a {
		t.Error("不同 ref 不应同 key")
	}
}
