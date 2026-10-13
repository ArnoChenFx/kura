package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// newTestConfig 建立隔离的临时环境：root + home 都在 t.TempDir() 下。
func newTestConfig(t *testing.T) *Config {
	t.Helper()
	root := filepath.Join(t.TempDir(), "skills")
	home := filepath.Join(t.TempDir(), "kura")
	c, err := NewConfig(root, home)
	if err != nil {
		t.Fatalf("构造配置失败: %v", err)
	}
	return c
}

// writeSkill 建一个最小技能目录（SKILL.md + 可选附加文件）。
func writeSkill(t *testing.T, root, name string, extra map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\n---\n\n# " + name
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content2 := range extra {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content2), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewConfigValidation(t *testing.T) {
	if _, err := NewConfig("", "x"); err == nil {
		t.Error("空 root 应报错")
	}
	if _, err := NewConfig("x", ""); err == nil {
		t.Error("空 home 应报错")
	}
}

func TestScanEmptyAndMissing(t *testing.T) {
	c := newTestConfig(t)
	got, err := c.Scan()
	if err != nil || len(got) != 0 {
		t.Errorf("空/不存在根目录应返回空列表, got %v, err %v", got, err)
	}
}

func TestScanSkills(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "b-skill", nil)
	writeSkill(t, c.Root, "a-skill", map[string]string{"references/guide.md": "hello"})
	// 干扰项：普通文件与隐藏目录都应被跳过。
	os.WriteFile(filepath.Join(c.Root, "not-a-dir.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(c.Root, ".hidden"), 0o755)
	got, err := c.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应扫描到 2 个技能, got %d", len(got))
	}
	if got[0].Name != "a-skill" || got[1].Name != "b-skill" {
		t.Errorf("应按名字排序: %s, %s", got[0].Name, got[1].Name)
	}
	for _, s := range got {
		if !s.HasSKILLMD {
			t.Errorf("%s 应有 SKILL.md", s.Name)
		}
		if s.State != StateEnabled || s.Meta != nil || s.MetaErr != "" {
			t.Errorf("%s 状态错误: %+v", s.Name, s)
		}
		if s.ContentHash == "" || !strings.HasPrefix(s.ContentHash, "sha256:") {
			t.Errorf("%s 哈希格式错误: %q", s.Name, s.ContentHash)
		}
	}
}

func TestMetaRoundTrip(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "demo", nil)
	dir := c.SkillPath("demo")
	m := &Meta{
		ID:          "demo",
		DisplayName: "demo",
		Source:      Source{Type: SourceGitHub, URL: "https://github.com/o/r.git", Ref: "main", SubPath: "skills/demo", Commit: "abc1234"},
		PluginName:  "p",
		InstalledAt: "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-02T00:00:00Z",
		ContentHash: "sha256:aa",
		Notes:       "n",
	}
	if err := writeMeta(dir, m); err != nil {
		t.Fatal(err)
	}
	res := readMeta(dir)
	if !res.Found || res.Err != nil || res.Meta == nil {
		t.Fatalf("读回应成功: %+v", res)
	}
	got := res.Meta
	if got.MetaVersion != MetaVersion || got.ID != "demo" || got.Source.URL != "https://github.com/o/r.git" ||
		got.Source.SubPath != "skills/demo" || got.Source.Commit != "abc1234" || got.PluginName != "p" {
		t.Errorf("往返内容不一致: %+v", got)
	}
	if got.ContentHash != "sha256:aa" || got.Notes != "n" {
		t.Errorf("可选字段往返不一致: %+v", got)
	}
}

func TestReadMetaMissingAndCorrupt(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "plain", nil)
	res := readMeta(c.SkillPath("plain"))
	if res.Found || res.Err != nil {
		t.Errorf("无 meta 文件应 Found=false 且无错误: %+v", res)
	}
	writeSkill(t, c.Root, "broken", nil)
	dir := c.SkillPath("broken")
	os.WriteFile(filepath.Join(dir, MetaFileName), []byte("{not json"), 0o644)
	res = readMeta(dir)
	if !res.Found || res.Err == nil || res.Meta != nil {
		t.Errorf("损坏 meta 应 Found=true 且有错误: %+v", res)
	}
}

func TestScanCorruptMetaDegrades(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "broken", nil)
	os.WriteFile(filepath.Join(c.Root, "broken", MetaFileName), []byte("{oops"), 0o644)
	got, err := c.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("损坏 meta 不应影响扫描, got %d", len(got))
	}
	if got[0].Meta != nil || got[0].MetaErr == "" {
		t.Errorf("损坏 meta 应降级: %+v", got[0])
	}
}

func TestHashDir(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "h", map[string]string{"a.txt": "1", "sub/b.txt": "2"})
	dir := c.SkillPath("h")
	h1, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := HashDir(dir)
	if h1 != h2 {
		t.Fatalf("同一内容哈希应稳定: %s vs %s", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("哈希应有 sha256: 前缀: %s", h1)
	}
	// 修改一个文件 → 哈希必须变化。
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1-changed"), 0o644)
	h3, _ := HashDir(dir)
	if h3 == h1 {
		t.Error("内容变化后哈希应变化")
	}
	// .kura-meta.json 不参与哈希。
	writeMeta(dir, &Meta{ID: "h", DisplayName: "h", Source: Source{Type: SourceLocal}})
	h4, _ := HashDir(dir)
	if h4 != h3 {
		t.Error("meta 文件不应影响内容哈希")
	}
}

func TestHashDirPathOrderInsensitive(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "ord", map[string]string{"z.txt": "z", "a/a.txt": "a", "m/m.txt": "m"})
	h1, _ := HashDir(c.SkillPath("ord"))
	// 直接重新哈希应一致（内部已排序）。
	h2, _ := HashDir(c.SkillPath("ord"))
	if h1 != h2 {
		t.Error("路径顺序不应影响哈希")
	}
}

func TestImportMeta(t *testing.T) {
	c := newTestConfig(t)
	lockDir := t.TempDir()
	lockPath := filepath.Join(lockDir, ".skill-lock.json")
	lock := `{
		"version": 3,
		"skills": {
			"locked-skill": {
				"source": "owner/repo",
				"sourceType": "github",
				"sourceUrl": "https://github.com/owner/repo.git",
				"skillPath": "skills/engineering/locked-skill/SKILL.md",
				"installedAt": "2026-06-21T04:42:05.890Z",
				"updatedAt": "2026-09-06T13:34:08.150Z",
				"pluginName": "p-lock"
			},
			"local-skill": {
				"source": "E:\\src\\skill",
				"sourceType": "local",
				"skillPath": "SKILL.md",
				"installedAt": "2026-07-01T00:00:00Z",
				"updatedAt": "2026-07-01T00:00:00Z"
			},
			"not-installed": {"sourceType": "github", "source": "x/y"}
		}
	}`
	os.WriteFile(lockPath, []byte(lock), 0o644)
	writeSkill(t, c.Root, "locked-skill", nil)
	writeSkill(t, c.Root, "local-skill", nil)
	writeSkill(t, c.Root, "no-meta-no-lock", nil)
	writeSkill(t, c.Root, "has-meta", nil)
	writeMeta(c.SkillPath("has-meta"), &Meta{ID: "has-meta", DisplayName: "has-meta", Source: Source{Type: SourceLocal}})
	imported, err := c.ImportMeta(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(imported)
	want := []string{"local-skill", "locked-skill"}
	if !slices.Equal(imported, want) {
		t.Errorf("导入列表不符: got %v want %v", imported, want)
	}
	// 校验导入内容映射。
	res := readMeta(c.SkillPath("locked-skill"))
	m := res.Meta
	if m == nil || m.Source.Type != "github" || m.Source.URL != "https://github.com/owner/repo.git" ||
		m.Source.SubPath != "skills/engineering/locked-skill" || m.PluginName != "p-lock" ||
		m.InstalledAt != "2026-06-21T04:42:05.890Z" {
		t.Errorf("导入元数据不符: %+v", m)
	}
	res = readMeta(c.SkillPath("local-skill"))
	m = res.Meta
	if m == nil || m.Source.Type != "local" || m.Source.Path != `E:\src\skill` {
		t.Errorf("本地来源导入不符: %+v", m)
	}
	// has-meta 的已有元数据不能被覆盖。
	res = readMeta(c.SkillPath("has-meta"))
	if res.Meta == nil || res.Meta.PluginName != "" {
		t.Errorf("已有元数据被覆盖: %+v", res.Meta)
	}
}

func TestDisableEnable(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "sw", nil)
	writeMeta(c.SkillPath("sw"), &Meta{ID: "sw", DisplayName: "sw", Source: Source{Type: SourceLocal}})

	// Disable:根目录 → disabled,纯移动。
	r, err := c.Disable("sw")
	if err != nil || r.State != StateDisabled || r.Dir != c.DisabledPath("sw") {
		t.Fatalf("Disable 失败: %v %+v", err, r)
	}
	if fileExists(c.SkillPath("sw")) || !fileExists(c.DisabledPath("sw")) {
		t.Fatal("文件夹应已从根目录移到 disabled")
	}
	res := readMeta(c.DisabledPath("sw"))
	if res.Meta == nil || res.Meta.ID != "sw" {
		t.Error("meta 应随文件夹一起移动")
	}
	dis, _ := c.ScanDisabled()
	if len(dis) != 1 || dis[0].Name != "sw" {
		t.Errorf("ScanDisabled 应看到 sw: %+v", dis)
	}

	// Enable:移回根目录。
	r, err = c.Enable("sw")
	if err != nil || r.State != StateEnabled || r.Dir != c.SkillPath("sw") {
		t.Fatalf("Enable 失败: %v %+v", err, r)
	}
	if !fileExists(c.SkillPath("sw")) || fileExists(c.DisabledPath("sw")) {
		t.Error("文件夹应已移回根目录")
	}

	// 状态机校验：回根目录后可再次 Disable；disabled 已空时 Enable 报错。
	if r2, err := c.Disable("sw"); err != nil || r2.State != StateDisabled {
		t.Errorf("重新 Disable 应成功: %v %+v", err, r2)
	}
	if r3, err := c.Enable("sw"); err != nil || r3.State != StateEnabled {
		t.Errorf("再次 Enable 应成功: %v %+v", err, r3)
	}
	// ghost 技能无论哪个操作都返回 ErrUnknownSkill。
	for _, tc := range []struct {
		name string
		fn   func(string) (*Result, error)
	}{
		{"Disable", c.Disable}, {"Remove", c.Remove}, {"Update", c.Update}, {"Enable", c.Enable},
	} {
		if _, err := tc.fn("ghost"); !errors.Is(err, ErrUnknownSkill) {
			t.Errorf("%s(ghost) 应返回 ErrUnknownSkill, got %v", tc.name, err)
		}
	}
	if _, err := c.Disable("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("Disable 未知技能应返回 ErrUnknownSkill, got %v", err)
	}
}

func TestRemoveRestore(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "gone", map[string]string{"ref/x.md": "x"})
	writeMeta(c.SkillPath("gone"), &Meta{ID: "gone", DisplayName: "gone", Source: Source{Type: SourceLocal}})
	hashBefore, _ := HashDir(c.SkillPath("gone"))

	r, err := c.Remove("gone")
	if err != nil || r.State != StateTrashed {
		t.Fatalf("Remove 失败: %v %+v", err, r)
	}
	if !fileExists(r.Dir) || fileExists(c.SkillPath("gone")) {
		t.Error("技能应已进入 trash")
	}
	// 快照必须已写入，且内容与删除前一致。
	if r.SnapshotDir == "" || !fileExists(r.SnapshotDir) {
		t.Fatal("Remove 应先写入快照")
	}
	snapHash, _ := HashDir(r.SnapshotDir)
	if snapHash != hashBefore {
		t.Errorf("快照内容不一致: %s vs %s", snapHash, hashBefore)
	}
	// 回收站结构 trash/<name>/<stamp>。
	trashed, _ := c.ScanTrash()
	if len(trashed) != 1 || trashed[0].Name != "gone" {
		t.Errorf("ScanTrash 应看到 gone: %+v", trashed)
	}

	// 根目录有同名技能时 Restore 应拒绝。
	writeSkill(t, c.Root, "gone", nil)
	if _, err := c.Restore("gone", ""); err == nil {
		t.Error("根目录已存在同名技能时 Restore 应报错")
	}
	os.RemoveAll(c.SkillPath("gone"))

	// 恢复(不指定 stamp,取最新)。
	r, err = c.Restore("gone", "")
	if err != nil || r.State != StateEnabled {
		t.Fatalf("Restore 失败: %v %+v", err, r)
	}
	if !fileExists(filepath.Join(c.SkillPath("gone"), "SKILL.md")) {
		t.Error("恢复后 SKILL.md 应存在")
	}
	hashAfter, _ := HashDir(c.SkillPath("gone"))
	if hashAfter != hashBefore {
		t.Errorf("恢复后内容不一致: %s vs %s", hashAfter, hashBefore)
	}
	if _, err := c.Restore("gone", ""); err == nil {
		t.Error("再次 Restore 应报错(回收站已空)")
	}
}

// initGitRepo 建一个本地 git 仓库（bare=false,便于 file:// clone）,
// 内容为 skills/<name>/SKILL.md,返回仓库根目录。
func initGitRepo(t *testing.T, skillName, skillBody string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	skillDir := filepath.Join(repo, "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + skillName + "\n---\n\n" + skillBody
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return repo
}

func TestAddLocal(t *testing.T) {
	c := newTestConfig(t)
	src := filepath.Join(t.TempDir(), "my-skill")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: my-skill\n---\nhi"), 0o644)
	os.WriteFile(filepath.Join(src, "extra.txt"), []byte("e"), 0o644)

	r, err := c.Add(&AddOptions{LocalPath: src})
	if err != nil || r.State != StateEnabled || r.ID != "my-skill" {
		t.Fatalf("Add(local) 失败: %v %+v", err, r)
	}
	if !fileExists(filepath.Join(c.Root, "my-skill", "extra.txt")) {
		t.Error("附加文件应已复制")
	}
	res := readMeta(c.SkillPath("my-skill"))
	m := res.Meta
	if m == nil || m.Source.Type != "local" || m.Source.Path != src {
		t.Errorf("local 元数据不符: %+v", m)
	}
	if m.InstalledAt == "" || m.ContentHash == "" || !strings.HasPrefix(m.ContentHash, "sha256:") {
		t.Errorf("安装时间/哈希缺失: %+v", m)
	}

	// 重复安装、参数错误、缺少 SKILL.md 的来源都应报错。
	if _, err := c.Add(&AddOptions{LocalPath: src}); err == nil {
		t.Error("重复安装应报错")
	}
	if _, err := c.Add(&AddOptions{}); err == nil {
		t.Error("无来源应报错")
	}
	if _, err := c.Add(&AddOptions{LocalPath: src, Repo: "a/b"}); err == nil {
		t.Error("双来源应报错")
	}
	noskill := filepath.Join(t.TempDir(), "noskill")
	os.MkdirAll(noskill, 0o755)
	if _, err := c.Add(&AddOptions{LocalPath: noskill}); err == nil {
		t.Error("缺少 SKILL.md 应报错")
	}
}

func TestAddGitHub(t *testing.T) {
	c := newTestConfig(t)
	// 用本地 bare 仓库模拟 github clone:把 owner/repo 指到 file:// URL 不可行,
	// 因此借 git 的 insteadOf 重写 URL。
	repo := initGitRepo(t, "gh-skill", "v1")
	bare := filepath.Join(t.TempDir(), "bare.git")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("clone", "-q", "--bare", repo, bare)
	// git -c url.<base>.insteadOf=git@github.com: 在 clone 时重写 URL。
	// store 包内部用 https://github.com/owner/repo.git,配置全局 insteadOf 指向本地 bare。
	cmd := exec.Command("git", "config", "--global", "url."+bare+".insteadOf", "https://github.com/owner/repo.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("配置 insteadOf 失败: %v: %s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("git", "config", "--global", "--unset", "url."+bare+".insteadOf").Run()
	})

	r, err := c.Add(&AddOptions{Repo: "owner/repo", SubPath: "skills/gh-skill", Ref: ""})
	if err != nil {
		t.Fatalf("Add(github) 失败: %v", err)
	}
	if r.ID != "gh-skill" {
		t.Fatalf("技能名应取 SubPath 尾段: %+v", r)
	}
	res := readMeta(c.SkillPath("gh-skill"))
	m := res.Meta
	if m == nil || m.Source.Type != "github" || m.Source.URL != "https://github.com/owner/repo.git" ||
		m.Source.SubPath != "skills/gh-skill" {
		t.Errorf("github 元数据不符: %+v", m)
	}
	if m.Source.Commit == "" {
		t.Error("应记录 commit")
	}
	if fileExists(filepath.Join(c.Root, "gh-skill", ".git")) {
		t.Error(".git 不应被复制进技能")
	}

	// 仓库内不存在的 SubPath 应报错。
	if _, err := c.Add(&AddOptions{Repo: "owner/repo", SubPath: "skills/nope"}); err == nil {
		t.Error("不存在的 SubPath 应报错")
	}
	// 非法 Repo 格式应报错。
	if _, err := c.Add(&AddOptions{Repo: "bad-format"}); err == nil {
		t.Error("非法 Repo 应报错")
	}
}

// TestAddGitHubFailureLeavesNoTempDir 验证 git clone 失败时不遗留
// kura-clone-* 临时目录。PATH 指向空目录，git 必然找不到：既不联网，也不依赖
// 本机的 git 配置；临时目录指到 t.TempDir() 里以便检查。
func TestAddGitHubFailureLeavesNoTempDir(t *testing.T) {
	c := newTestConfig(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // Unix 的 os.TempDir
	t.Setenv("TMP", tmp)    // Windows 的 os.TempDir
	t.Setenv("TEMP", tmp)
	t.Setenv("PATH", t.TempDir())

	if _, err := c.Add(&AddOptions{Repo: "owner/repo"}); err == nil {
		t.Fatal("找不到 git 时 Add 应报错")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("clone 失败后不应遗留临时目录: %s", e.Name())
	}
}

func TestUpdate(t *testing.T) {
	c := newTestConfig(t)
	// 准备 git 仓库与 bare 镜像 + insteadOf 重写(同 TestAddGitHub)。
	repo := initGitRepo(t, "up-skill", "v1")
	bare := filepath.Join(t.TempDir(), "bare.git")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("clone", "-q", "--bare", repo, bare)
	exec.Command("git", "config", "--global", "url."+bare+".insteadOf", "https://github.com/owner/repo2.git").Run()
	t.Cleanup(func() {
		exec.Command("git", "config", "--global", "--unset", "url."+bare+".insteadOf").Run()
	})

	// 安装 v1。
	if _, err := c.Add(&AddOptions{Repo: "owner/repo2", SubPath: "skills/up-skill"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	v1hash, _ := HashDir(c.SkillPath("up-skill"))
	m0 := readMeta(c.SkillPath("up-skill")).Meta
	if m0 == nil {
		t.Fatal("安装后应有元数据")
	}

	// 上游推进一个 commit(v2)。
	cd := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(repo, "skills", "up-skill", "SKILL.md"), []byte("---\nname: up-skill\n---\nv2"), 0o644)
	cd(repo, "add", ".")
	cd(repo, "commit", "-q", "-m", "v2")
	run("push", "-q", repo+"/.git", "HEAD:refs/heads/main")
	run("--git-dir", bare, "rev-parse", "HEAD")

	// bare 仓库是快照,clone 的 insteadOf 指向它,但 bare 不会自动同步 v2。
	// 重新镜像一次 v2。
	os.RemoveAll(bare)
	run("clone", "-q", "--bare", repo, bare)

	// updatedAt 精确到秒：等过一个整秒，保证「前进」可观测（快机器上
	// 安装与更新可能落在同一秒）。
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))

	// Update。
	r, err := c.Update("up-skill")
	if err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if r.State != StateEnabled || r.SnapshotDir == "" {
		t.Errorf("Update 应返回快照目录: %+v", r)
	}
	if !fileExists(r.SnapshotDir) {
		t.Error("旧内容应已快照")
	}
	v2hash, _ := HashDir(c.SkillPath("up-skill"))
	if v1hash == v2hash {
		t.Error("更新后内容哈希应变化")
	}
	m1 := readMeta(c.SkillPath("up-skill")).Meta
	if m1 == nil || m1.UpdatedAt <= m0.UpdatedAt {
		t.Errorf("updatedAt 应前进: %s -> %s", m0.UpdatedAt, m1.UpdatedAt)
	}
	if m1.InstalledAt != m0.InstalledAt {
		t.Error("installedAt 应保持不变")
	}
	if m1.Source.Commit == m0.Source.Commit {
		t.Error("commit 应前进")
	}
	if m1.ContentHash != v2hash {
		t.Errorf("元数据哈希应与新内容一致")
	}

	// 未知技能 Update 报错。
	if _, err := c.Update("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("Update(ghost) 应返回 ErrUnknownSkill, got %v", err)
	}
}
