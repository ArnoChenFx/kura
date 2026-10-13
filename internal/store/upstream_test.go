package store

// 轻量检查更新与同仓库复用 clone 的测试。全部在 t.TempDir() 里用本地 git
// 仓库构造（经 gitMirror 的 insteadOf 冒充 github 地址），无网络。

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// plainRepo 在 base/repo 建一个 git 仓库，文件由 files 表（slash 相对路径 → 内容）驱动。
func plainRepo(t *testing.T, base string, files map[string]string) string {
	t.Helper()
	repo := filepath.Join(base, "repo")
	for rel, content := range files {
		writeRepoFile(t, repo, rel, content)
	}
	gitCmd(t, repo, "init", "-q")
	gitCmd(t, repo, "config", "user.email", "t@t")
	gitCmd(t, repo, "config", "user.name", "t")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// 不给子目录安装：先只检出发现要用的文件，选定技能后再检出它的整个目录，
// 元数据记下子目录与树哈希，技能目录里没有仓库里的其他东西。
func TestAddDiscoveryThenSkillCheckout(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := plainRepo(t, t.TempDir(), map[string]string{
		"skills/alpha/SKILL.md":       "---\nname: alpha\n---\n\nalpha\n",
		"skills/alpha/refs/extra.md":  "extra\n",
		"skills/alpha/scripts/run.sh": "echo hi\n",
		"docs/unrelated.md":           "unrelated\n",
		"assets/data.bin":             "data\n",
	})
	newMirror(t, repo, "disc1")

	r, err := c.Add(&AddOptions{Repo: "owner/disc1"})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	for _, rel := range []string{"SKILL.md", "refs/extra.md", "scripts/run.sh"} {
		if !fileExists(filepath.Join(r.Dir, filepath.FromSlash(rel))) {
			t.Errorf("技能目录应有 %s", rel)
		}
	}
	for _, rel := range []string{"docs", "assets", ".git"} {
		if fileExists(filepath.Join(r.Dir, rel)) {
			t.Errorf("技能目录里不应有 %s", rel)
		}
	}
	m := readMeta(r.Dir).Meta
	if m.Source.SubPath != "skills/alpha" {
		t.Errorf("SubPath 应为 skills/alpha, got %q", m.Source.SubPath)
	}
	if want := previewGitOut(t, repo, "rev-parse", "HEAD:skills/alpha"); m.Source.Tree != want {
		t.Errorf("tree 应为 %s, got %q", want, m.Source.Tree)
	}
	if want := previewGitOut(t, repo, "rev-parse", "HEAD"); m.Source.Commit != want {
		t.Errorf("commit 应为 %s, got %q", want, m.Source.Commit)
	}
}

// 插件清单只放在仓库根的 .claude-plugin/plugin.json：只检出发现用的文件时
// 也要读得到它，并且清单排除了草稿目录。
func TestAddDiscoveryReadsManifest(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo := plainRepo(t, t.TempDir(), map[string]string{
		"skills/published/alpha/SKILL.md": "---\nname: alpha\n---\n\nalpha\n",
		"skills/draft/beta/SKILL.md":      "---\nname: beta\n---\n\nbeta\n",
		".claude-plugin/plugin.json":      `{"skills":["./skills/published/alpha"]}`,
	})
	newMirror(t, repo, "disc2")
	r, err := c.Add(&AddOptions{Repo: "owner/disc2"})
	if err != nil {
		t.Fatalf("清单只声明了一个技能，应直接安装它: %v", err)
	}
	if filepath.Base(r.Dir) != "alpha" {
		t.Errorf("应安装 alpha, got %s", r.Dir)
	}

	// 多个候选时列出候选，不安装任何东西。
	c2 := newTestConfig(t)
	repo2 := plainRepo(t, t.TempDir(), map[string]string{
		"skills/one/SKILL.md": "---\nname: one\n---\n",
		"skills/two/SKILL.md": "---\nname: two\n---\n",
	})
	newMirror(t, repo2, "disc3")
	_, err = c2.Add(&AddOptions{Repo: "owner/disc3"})
	var ce *CandidatesError
	if !errors.As(err, &ce) || len(ce.Paths) != 2 {
		t.Fatalf("应返回两个候选, got %v", err)
	}
	cands, err := c2.DiscoverRepoSkills("owner/disc3", "")
	if err != nil || len(cands) != 2 {
		t.Errorf("DiscoverRepoSkills 应列出 2 个候选: %v %v", cands, err)
	}
}

// 发现阶段也不下载技能无关的大文件：只取 SKILL.md 和清单（通过 file:// 走真实部分 clone）。
func TestDiscoveryCheckoutSkipsUnrelatedBlobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file:// URL 的路径写法因平台而异，只在 Unix 上验证")
	}
	isolatedGitEnv(t)
	big := make([]byte, 2<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	repo := plainRepo(t, t.TempDir(), map[string]string{
		"skills/one/SKILL.md": "---\nname: one\n---\n",
		"skills/two/SKILL.md": "---\nname: two\n---\n",
		"assets/big.bin":      string(big),
	})
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitCmd(t, "", "clone", "-q", "--bare", repo, bare)
	gitCmd(t, bare, "config", "uploadpack.allowFilter", "true")
	key := "url.file://" + bare + ".insteadOf"
	gitCmd(t, "", "config", "--global", key, "https://github.com/owner/disc4.git")
	t.Cleanup(func() { gitCmd(t, "", "config", "--global", "--unset", key) })

	c := newTestConfig(t)
	u, _, err := c.resolveGitHub(&AddOptions{Repo: "owner/disc4"})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	dr, err := DiscoverSkills(u.dir)
	if err != nil || len(dr.Paths) != 2 {
		t.Fatalf("应发现两个技能: %+v %v", dr, err)
	}
	if size := dirSize(t, filepath.Join(u.dir, ".git")); size > 512<<10 {
		t.Errorf("发现阶段不应下载无关的大文件，.git 有 %d 字节", size)
	}
	if fileExists(filepath.Join(u.dir, "assets")) {
		t.Error("发现阶段不应检出 assets")
	}
}

// 路径里有空格和稀疏检出/gitignore 的特殊字符：只检出这个目录本身，
// 不会把名字匹配通配符的兄弟目录也带出来；树哈希按字面路径取。
func TestUpstreamSpecialCharPaths(t *testing.T) {
	isolatedGitEnv(t)
	const weird = "skills/we[ird] #name!x"
	repo := plainRepo(t, t.TempDir(), map[string]string{
		weird + "/SKILL.md":           "weird\n",
		weird + "/refs/a.md":          "a\n",
		"skills/wei #name!x/SKILL.md": "sibling matched by an unescaped [ird]\n",
		"skills/plain/SKILL.md":       "plain\n",
	})
	newMirror(t, repo, "weird1")
	u, err := cloneUpstream("https://github.com/owner/weird1.git", "")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	want := previewGitOut(t, repo, "rev-parse", "HEAD:"+weird)
	if got, err := u.Tree(weird); err != nil || got != want {
		t.Fatalf("Tree(%q) = %q, %v; want %s", weird, got, err, want)
	}
	if err := u.Checkout([]string{weird}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(u.Dir(weird), "refs", "a.md")) {
		t.Error("应检出特殊字符目录里的文件")
	}
	for _, other := range []string{"skills/wei #name!x", "skills/plain"} {
		if fileExists(filepath.Join(u.dir, filepath.FromSlash(other))) {
			t.Errorf("不应检出 %s", other)
		}
	}
}

// git 命令卡住（服务器接受连接却不响应）时按超时返回 ErrGitTimeout，而不是一直挂着。
func TestGitTimeoutAgainstStalledServer(t *testing.T) {
	isolatedGitEnv(t)
	t.Setenv("no_proxy", "*")
	t.Setenv("NO_PROXY", "*")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听本地端口: %v", err)
	}
	defer ln.Close()
	var conns []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c) // 接受后不读不写，模拟卡死的网络
			mu.Unlock()
		}
	}()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	}()
	oldLs, oldClone, oldWait := lsRemoteTimeout, cloneTimeout, gitWaitDelay
	lsRemoteTimeout, cloneTimeout, gitWaitDelay = 500*time.Millisecond, 500*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { lsRemoteTimeout, cloneTimeout, gitWaitDelay = oldLs, oldClone, oldWait })

	url := "http://" + ln.Addr().String() + "/stalled.git"
	start := time.Now()
	if _, err := remoteHead(url, ""); !errors.Is(err, ErrGitTimeout) {
		t.Errorf("ls-remote 卡住应返回 ErrGitTimeout, got %v", err)
	}
	if _, err := cloneUpstream(url, ""); !errors.Is(err, ErrGitTimeout) {
		t.Errorf("clone 卡住应返回 ErrGitTimeout, got %v", err)
	}
	// 两次调用各 0.5s 超时 + 0.3s 宽限；给慢 CI 留足余量，但远小于 lowSpeedTime 的 30s。
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("超时应在限定时间附近返回, 实际用了 %s", elapsed)
	}
}

// 路径里的换行会在稀疏检出模式文件里注入额外的一行（例如 /*，把整个仓库检出来），
// Trees 与 Checkout 都要在碰 git 之前拒绝它。
func TestNewlinePathsAreRejected(t *testing.T) {
	u := &upstream{dir: t.TempDir()}
	const evil = "skills/x\n/*"
	if _, err := u.Tree(evil); err == nil {
		t.Error("Tree 应拒绝带换行的路径")
	}
	if err := u.Checkout([]string{"skills/ok", evil}); err == nil {
		t.Error("Checkout 应拒绝带换行的路径")
	}
	if fileExists(filepath.Join(u.dir, ".git", "info", "sparse-checkout")) {
		t.Error("被拒绝的 Checkout 不应写出稀疏检出文件")
	}
}
