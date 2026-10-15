package store

// add.go 的两组测试：
//   - 批量安装（AddMany）：同一个仓库只 clone 一次、逐项结果、单项失败不影响
//     其他项、不遗漏 commit/树哈希；
//   - 默认技能名：优先 SKILL.md frontmatter 的 name，它不能当目录名用时退回
//     来源目录名（仓库根本身是技能时退回仓库名）。
//
// 全部场景都在 t.TempDir() 里用本地假仓库（经 installMirror 的 insteadOf 冒充
// github 地址）与本地目录构造，不联网、不碰真实的 ~/.agents/skills 与 ~/.kura。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// multiSkillRepo 造一个含若干技能的仓库（slash 相对目录，通常写成
// skills/<name>），提交一次并镜像成 github.com/owner/<alias>。
func multiSkillRepo(t *testing.T, alias string, skills ...string) string {
	t.Helper()
	repo := t.TempDir()
	for _, s := range skills {
		repoSkill(t, repo, s)
	}
	writeRepoFile(t, repo, "README.md", "仓库说明\n")
	commitRepo(t, repo)
	restore := installMirror(t, repo, alias)
	t.Cleanup(restore)
	return repo
}

// TestAddManyInstallsAllWithOneClone 验证同一批多个技能只 clone 一次、
// 全部装上，且每项都记下 SubPath / commit / 树哈希，clone 的 .git 不落进技能。
func TestAddManyInstallsAllWithOneClone(t *testing.T) {
	multiSkillRepo(t, "owner/addmany",
		"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me")
	c := newTestConfig(t)
	clones := countClones(t)

	specs := []AddSpec{
		{SubPath: "skills/engineering/tdd"},
		{SubPath: "skills/engineering/code-review"},
		{SubPath: "skills/productivity/grill-me"},
	}
	results := c.AddMany("owner/addmany", "", specs)
	if len(results) != len(specs) {
		t.Fatalf("结果应与入参一一对应，得到 %d 项", len(results))
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("第 %d 项应成功（%s）: %v", i, r.SubPath, r.Err)
		}
		if r.SubPath != specs[i].SubPath {
			t.Errorf("第 %d 项的 SubPath 应原样回填，得到 %q", i, r.SubPath)
		}
		name := filepath.Base(specs[i].SubPath)
		if r.Name != name || r.Result == nil || r.Result.ID != name || r.Result.State != StateEnabled {
			t.Errorf("第 %d 项结果不符：Name=%q Result=%+v", i, r.Name, r.Result)
		}
	}
	if *clones != 1 {
		t.Errorf("同一批技能应只 clone 一次，得到 %d 次", *clones)
	}
	for _, spec := range specs {
		name := filepath.Base(spec.SubPath)
		dir := c.SkillPath(name)
		if !fileExists(filepath.Join(dir, "SKILL.md")) {
			t.Errorf("%s 应已装上", name)
		}
		if fileExists(filepath.Join(dir, ".git")) {
			t.Errorf("%s 里不应带 .git", name)
		}
		m := readMeta(dir).Meta
		if m == nil || m.Source.Type != SourceGitHub || m.Source.SubPath != spec.SubPath ||
			m.Source.Commit == "" || m.Source.Tree == "" || m.ContentHash == "" {
			t.Errorf("%s 的元数据不符: %+v", name, m)
		}
	}
}

// TestAddManyPartialFailureKeepsOthers 验证单项失败不影响其他项：中间那个目标
// 目录已被占用，前后两个照装，失败的 Err 里看得出是哪个子目录、为什么。
func TestAddManyPartialFailureKeepsOthers(t *testing.T) {
	multiSkillRepo(t, "owner/partial", "skills/a", "skills/b", "skills/c")
	c := newTestConfig(t)
	writeSkill(t, c.Root, "b", nil) // 预先占用 b：装 skills/b 时必然撞名
	clones := countClones(t)

	results := c.AddMany("owner/partial", "", []AddSpec{
		{SubPath: "skills/a"}, {SubPath: "skills/b"}, {SubPath: "skills/c"},
	})
	if results[0].Err != nil || results[2].Err != nil {
		t.Fatalf("a 与 c 应照装：%v / %v", results[0].Err, results[2].Err)
	}
	if results[0].Name != "a" || results[2].Name != "c" {
		t.Errorf("装成的名字应是 a 与 c，得到 %q / %q", results[0].Name, results[2].Name)
	}
	if results[1].Err == nil {
		t.Fatal("b 应因根目录同名冲突失败")
	}
	if msg := results[1].Err.Error(); !strings.Contains(msg, "skills/b") || !strings.Contains(msg, "已存在同名技能") {
		t.Errorf("失败信息应指出是哪个技能、为什么：%s", msg)
	}
	if results[1].Name != "" || results[1].Result != nil {
		t.Errorf("失败项不应有名字与结果：%+v", results[1])
	}
	// 预先存在的 b 没有被覆盖（它的 SKILL.md 还是 writeSkill 写的那份）。
	if got, _ := os.ReadFile(filepath.Join(c.SkillPath("b"), "SKILL.md")); !strings.Contains(string(got), "name: b") {
		t.Errorf("已存在的 b 不应被覆盖: %q", got)
	}
	if *clones != 1 {
		t.Errorf("失败一项也应只 clone 一次，得到 %d 次", *clones)
	}
}

// TestAddManyMissingSubPathFailsOnlyThatItem 验证上游没有的子目录只让自己失败。
func TestAddManyMissingSubPathFailsOnlyThatItem(t *testing.T) {
	multiSkillRepo(t, "owner/missing", "skills/a", "skills/b")
	c := newTestConfig(t)

	results := c.AddMany("owner/missing", "", []AddSpec{
		{SubPath: "skills/a"}, {SubPath: "skills/nope"}, {SubPath: "skills/b"},
	})
	if results[0].Err != nil || results[2].Err != nil {
		t.Fatalf("a 与 b 应照装：%v / %v", results[0].Err, results[2].Err)
	}
	if results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "skills/nope") {
		t.Errorf("不存在的子目录应只让自己失败并指出路径，得到 %v", results[1].Err)
	}
	if entries, _ := os.ReadDir(c.Root); len(entries) != 2 {
		t.Errorf("只应装上两个技能，得到 %v", entries)
	}
}

// TestAddManyRejectsUnsafeSubPathPerItem 验证越界（..）的子目录在 clone 之前
// 就被挡下，且不影响同一批里的其他技能。
func TestAddManyRejectsUnsafeSubPathPerItem(t *testing.T) {
	multiSkillRepo(t, "owner/unsafe", "skills/a")
	c := newTestConfig(t)

	results := c.AddMany("owner/unsafe", "", []AddSpec{
		{SubPath: "../escape"}, {SubPath: "skills/a"},
	})
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "..") {
		t.Errorf("越界的子目录应报错，得到 %v", results[0].Err)
	}
	if results[1].Err != nil {
		t.Errorf("同一批里的正常技能应照装: %v", results[1].Err)
	}
	if fileExists(filepath.Join(filepath.Dir(c.Root), "escape")) {
		t.Error("越界安装不应在根目录之外留下任何文件夹")
	}
}

// TestAddManyInvalidRepoFailsEveryItem 验证来源格式错误时每项都带上原因。
func TestAddManyInvalidRepoFailsEveryItem(t *testing.T) {
	c := newTestConfig(t)
	results := c.AddMany("bad-format", "", []AddSpec{{SubPath: "skills/a"}, {SubPath: "skills/b"}})
	if len(results) != 2 {
		t.Fatalf("结果应与入参一一对应，得到 %d 项", len(results))
	}
	for i, r := range results {
		if r.Err == nil || !strings.Contains(r.Err.Error(), "owner/repo") {
			t.Errorf("第 %d 项应报来源格式错误，得到 %v", i, r.Err)
		}
	}
}

// TestAddManyEmptySpecs 验证空名单不做任何事（不 clone）。
func TestAddManyEmptySpecs(t *testing.T) {
	c := newTestConfig(t)
	clones := countClones(t)
	if results := c.AddMany("owner/none", "", nil); len(results) != 0 {
		t.Errorf("空名单应返回空结果，得到 %v", results)
	}
	if *clones != 0 {
		t.Errorf("空名单不应 clone，得到 %d 次", *clones)
	}
}

// TestAddManyDuplicateSubPathFailsSecond 记录「同一个子目录给两次」的既定行为
// （见 AddMany 的注释）：第一项装上，第二项报同名冲突——调用方不该重复给；
// 给了不同的 Name 时两项都能装上（见 TestAddManyNameSources）。
func TestAddManyDuplicateSubPathFailsSecond(t *testing.T) {
	multiSkillRepo(t, "owner/dupe", "skills/a")
	c := newTestConfig(t)

	results := c.AddMany("owner/dupe", "", []AddSpec{{SubPath: "skills/a"}, {SubPath: "skills/a"}})
	if results[0].Err != nil || results[0].Name != "a" {
		t.Fatalf("第一项应装上: %+v", results[0])
	}
	if results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "已存在同名技能") {
		t.Errorf("第二项应报同名冲突，得到 %v", results[1].Err)
	}
	if results[1].Name != "" || results[1].Result != nil {
		t.Errorf("失败项不应有名字与结果：%+v", results[1])
	}
}

// TestAddManyNameSources 验证批量安装的命名优先级：显式 Name > SKILL.md 的
// name > 来源目录名；同一个子目录选两次只检出一次。
func TestAddManyNameSources(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "skills/engineering/tdd/SKILL.md", "---\nname: test-driven-development\n---\n\n# 技能\n")
	writeRepoFile(t, repo, "skills/engineering/plain/SKILL.md", "---\nname: \"quoted name\"\n---\n\n# 技能\n")
	writeRepoFile(t, repo, "skills/engineering/noname/SKILL.md", "# 没有 frontmatter\n")
	commitRepo(t, repo)
	restore := installMirror(t, repo, "owner/names")
	t.Cleanup(restore)

	c := newTestConfig(t)
	clones := countClones(t)
	results := c.AddMany("owner/names", "", []AddSpec{
		{SubPath: "skills/engineering/tdd"},
		{SubPath: "skills/engineering/plain"},
		{SubPath: "skills/engineering/noname"},
		{SubPath: "skills/engineering/tdd", Name: "explicit"},
	})
	want := []string{"test-driven-development", "quoted name", "noname", "explicit"}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("第 %d 项失败（%s）: %v", i, r.SubPath, r.Err)
		}
		if r.Name != want[i] {
			t.Errorf("第 %d 项应装成 %q，得到 %q", i, want[i], r.Name)
		}
		if !fileExists(filepath.Join(c.SkillPath(want[i]), "SKILL.md")) {
			t.Errorf("%s 应已装上", want[i])
		}
	}
	if *clones != 1 {
		t.Errorf("只应 clone 一次，得到 %d 次", *clones)
	}
}

// rootSkillRepo 造一个「仓库根本身就是技能」的仓库（SKILL.md 的内容由 body
// 给出），提交并镜像成 github.com/<alias>。
func rootSkillRepo(t *testing.T, alias, body string) {
	t.Helper()
	repo := t.TempDir()
	writeRepoFile(t, repo, "SKILL.md", body)
	commitRepo(t, repo)
	restore := installMirror(t, repo, alias)
	t.Cleanup(restore)
}

// TestAddManyRepoRootSkillName 验证技能就是仓库根、frontmatter 的 name 可用时
// 默认名取它（而不是仓库名，也不是临时的 clone 目录名）。
func TestAddManyRepoRootSkillName(t *testing.T) {
	rootSkillRepo(t, "owner/rootnamed", "---\nname: real-root-skill\n---\n\n# 技能\n")
	c := newTestConfig(t)

	results := c.AddMany("owner/rootnamed", "", []AddSpec{{SubPath: "."}})
	if results[0].Err != nil {
		t.Fatalf("仓库根技能应装上: %v", results[0].Err)
	}
	if results[0].Name != "real-root-skill" {
		t.Errorf("默认名应取 SKILL.md 的 name，得到 %q", results[0].Name)
	}
	if m := readMeta(c.SkillPath("real-root-skill")).Meta; m == nil || m.Source.SubPath != "" {
		t.Errorf("仓库根安装的 SubPath 应为空，得到 %+v", m)
	}
}

// TestAddManyRepoRootSkillFallback 验证技能就是仓库根、frontmatter 不可用时
// 退回仓库名（临时的 clone 目录名不能当技能名）。
func TestAddManyRepoRootSkillFallback(t *testing.T) {
	rootSkillRepo(t, "owner/rootplain", "---\nname: .\n---\n\n# 技能\n")
	c := newTestConfig(t)

	results := c.AddMany("owner/rootplain", "", []AddSpec{{SubPath: ""}})
	if results[0].Err != nil {
		t.Fatalf("仓库根技能应装上: %v", results[0].Err)
	}
	if results[0].Name != "rootplain" {
		t.Errorf("frontmatter 不可用时应退回仓库名，得到 %q", results[0].Name)
	}
}

// TestReadSkillNameFrontmatter 覆盖 frontmatter 里 name 的常见写法与无效情形
// （与 readDescription 同一套扫描规则）。
func TestReadSkillNameFrontmatter(t *testing.T) {
	cases := map[string]string{
		"---\nname: plain\n---\n":           "plain",
		"---\nname: \"quoted name\"\n---\n": "quoted name",
		"---\nname: 'single'\n---\n":        "single",
		"---\r\nname: crlf\r\n---\r\n":      "crlf",
		"---\nname: 空格在中间 名字\n---\n":        "空格在中间 名字",
		"---\nmeta:\n  name: nested\n---\n": "",
		"---\ndescription: 只有说明\n---\n":     "",
		"---\nname: a\nname: b\n---\n":      "a", // 取第一个
		"# 没有 frontmatter\nname: no\n":      "",
	}
	for body, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readSkillName(dir); got != want {
			t.Errorf("readSkillName(%q) = %q，想要 %q", body, got, want)
		}
	}
	if got := readSkillName(t.TempDir()); got != "" {
		t.Errorf("缺少 SKILL.md 应返回空串，得到 %q", got)
	}
}

// TestAddDefaultNameFromSkillMDName 验证默认名取 SKILL.md frontmatter 的 name
// （目录名与技能名不同也能对上），元数据的 ID/DisplayName 跟着走。
func TestAddDefaultNameFromSkillMDName(t *testing.T) {
	c := newTestConfig(t)
	src := filepath.Join(t.TempDir(), "tdd") // 目录名 tdd，技能名 test-driven-development
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"),
		[]byte("---\nname: test-driven-development\n---\n\n# 技能\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := c.Add(&AddOptions{LocalPath: src})
	if err != nil {
		t.Fatalf("Add(local) 失败: %v", err)
	}
	if r.ID != "test-driven-development" {
		t.Fatalf("默认名应取 SKILL.md 的 name，得到 %q", r.ID)
	}
	if !fileExists(filepath.Join(c.SkillPath("test-driven-development"), "SKILL.md")) {
		t.Error("应按技能名建目录")
	}
	if fileExists(c.SkillPath("tdd")) {
		t.Error("不应按来源目录名建目录")
	}
	m := readMeta(c.SkillPath("test-driven-development")).Meta
	if m == nil || m.ID != "test-driven-development" || m.DisplayName != "test-driven-development" {
		t.Errorf("元数据的 ID/DisplayName 应跟随技能名: %+v", m)
	}

	// 显式名字永远第一：给了 Name 就不看 frontmatter。
	src2 := filepath.Join(t.TempDir(), "dir-name")
	if err := os.MkdirAll(src2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src2, "SKILL.md"),
		[]byte("---\nname: from-frontmatter\n---\n\n# 技能\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := c.Add(&AddOptions{LocalPath: src2, Name: "explicit"})
	if err != nil {
		t.Fatalf("Add(local, --name) 失败: %v", err)
	}
	if r2.ID != "explicit" {
		t.Errorf("显式名字应优先于 frontmatter，得到 %q", r2.ID)
	}

	// 带引号的 name 按 unquote 的规则去引号。
	src3 := filepath.Join(t.TempDir(), "another-dir")
	if err := os.MkdirAll(src3, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src3, "SKILL.md"),
		[]byte("---\nname: \"quoted-name\"\n---\n\n# 技能\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r3, err := c.Add(&AddOptions{LocalPath: src3})
	if err != nil {
		t.Fatalf("Add(local, 引号名) 失败: %v", err)
	}
	if r3.ID != "quoted-name" {
		t.Errorf("带引号的 name 应去掉引号，得到 %q", r3.ID)
	}
}

// TestAddDefaultNameFallsBackToDirName 验证 frontmatter 的 name 不能当目录名
// 用时退回来源目录名——只是仓库里的 frontmatter 不讲究，不该让安装失败。
func TestAddDefaultNameFallsBackToDirName(t *testing.T) {
	cases := []struct {
		raw string // 直接拼在 "name: " 后面的原始值（可带引号）
		why string
	}{
		{`""`, "空名字"},
		{`.`, "\".\""},
		{`..`, "\"..\""},
		{`a/b`, "含 /"},
		{`a\b`, "含 \\"},
		{`a..b`, "含 .."},
		{`a<b`, "含 <"},
		{`a>b`, "含 >"},
		{`a:b`, "含 :"},
		{`a|b`, "含 |"},
		{`a?b`, "含 ?"},
		{`a*b`, "含 *"},
		{`"trail "`, "以空格结尾"},
		{`"trail."`, "以点结尾"},
		{`con`, "Windows 设备名"},
		{`CON.md`, "带扩展名的设备名"},
		{"\"a\x01b\"", "控制字符"},
	}
	for i, tc := range cases {
		dir := filepath.Join(t.TempDir(), fmt.Sprintf("fallback-%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + tc.raw + "\n---\n\n# 技能\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c := newTestConfig(t)
		r, err := c.Add(&AddOptions{LocalPath: dir})
		if err != nil {
			t.Fatalf("%s（%q）: Add 应退回目录名而不是报错: %v", tc.why, tc.raw, err)
		}
		if want := filepath.Base(dir); r.ID != want {
			t.Errorf("%s（%q）: 应退回目录名 %q，得到 %q", tc.why, tc.raw, want, r.ID)
		}
	}
}

// TestAddRepoRootSkillDefaultName 验证单个安装的仓库根技能：frontmatter 的
// name 可用就用它（不是仓库名，也不是临时的 clone 目录名）。
func TestAddRepoRootSkillDefaultName(t *testing.T) {
	rootSkillRepo(t, "owner/rootnamed", "---\nname: root-skill-name\n---\n\n# 技能\n")
	c := newTestConfig(t)

	r, err := c.Add(&AddOptions{Repo: "owner/rootnamed"})
	if err != nil {
		t.Fatalf("仓库根技能应直接安装: %v", err)
	}
	if r.ID != "root-skill-name" {
		t.Errorf("默认名应取 SKILL.md 的 name，得到 %q", r.ID)
	}
}

// TestAddRepoRootSkillFallbackToRepoName 验证单个安装的仓库根技能：frontmatter
// 不可用时退回仓库名（临时的 clone 目录名不能当技能名）。
func TestAddRepoRootSkillFallbackToRepoName(t *testing.T) {
	rootSkillRepo(t, "owner/rootplain", "---\nname: .\n---\n\n# 技能\n")
	c := newTestConfig(t)

	r, err := c.Add(&AddOptions{Repo: "owner/rootplain"})
	if err != nil {
		t.Fatalf("仓库根技能应直接安装: %v", err)
	}
	if r.ID != "rootplain" {
		t.Errorf("frontmatter 不可用时应退回仓库名，得到 %q", r.ID)
	}
}

// TestValidSkillDirName 逐个核对合法与非法目录名的判据。
func TestValidSkillDirName(t *testing.T) {
	valid := []string{"tdd", "code-review", "技能名", "a b", "a.b", "con-x", "COM10", "xCON"}
	for _, name := range valid {
		if !validSkillDirName(name) {
			t.Errorf("%q 应判为可用的目录名", name)
		}
	}
	invalid := []string{"", ".", "..", "a/b", `a\b`, "a..b", "a<b", "a>b", "a:b", "a|b", "a?b", "a*b",
		"trail ", "trail.", "con", "CON", "Con.md", "nul", "lpt9.txt", "a\x01b"}
	for _, name := range invalid {
		if validSkillDirName(name) {
			t.Errorf("%q 应判为不能当目录名", name)
		}
	}
}

// TestAddRepoRootSkillSubPathDot 验证子目录写成 "." 时按「仓库根」处理，与空串
// 完全等价。UI 的自动安装发现到「仓库根本身是技能」时会把候选路径 "." 填回
// 子目录（DiscoverSkills 用 "." 表示仓库根），这条路径必须能装上——否则
// 这类「根即技能」的仓库在界面里永远装不上（CLI 里手写
// `--sub-path .` 同样会失败）。
func TestAddRepoRootSkillSubPathDot(t *testing.T) {
	rootSkillRepo(t, "owner/rootdot", "---\nname: dot-root-skill\n---\n\n# 技能\n")
	c := newTestConfig(t)

	r, err := c.Add(&AddOptions{Repo: "owner/rootdot", SubPath: "."})
	if err != nil {
		t.Fatalf("子目录为 \".\" 时应按仓库根安装: %v", err)
	}
	if r.ID != "dot-root-skill" {
		t.Errorf("默认名应取 SKILL.md 的 name，得到 %q", r.ID)
	}
	m := readMeta(c.SkillPath("dot-root-skill")).Meta
	if m == nil || m.Source.SubPath != "" {
		t.Errorf("仓库根安装的 SubPath 应记为空串（不能是 \".\"），得到 %+v", m)
	}
}

// TestAddRepoRootSkillSubPathDotFallbackName 验证显式 "." 安装仓库根技能时，
// 默认名同样退回仓库名。临时 clone 目录名（kura-upstream-NNN）不能当技能名，
// 发现路径里有这条兜底，显式 "." 必须走同一套。
func TestAddRepoRootSkillSubPathDotFallbackName(t *testing.T) {
	rootSkillRepo(t, "owner/rootdotplain", "---\nname: .\n---\n\n# 技能\n")
	c := newTestConfig(t)

	r, err := c.Add(&AddOptions{Repo: "owner/rootdotplain", SubPath: "."})
	if err != nil {
		t.Fatalf("仓库根技能应能安装: %v", err)
	}
	if r.ID != "rootdotplain" {
		t.Errorf("frontmatter 不可用时应退回仓库名，得到 %q", r.ID)
	}
}

// TestAddManyRepoRootSkillSubPathDot 验证批量安装里 "." 与空串等价：
// 两处都按仓库根装上（一个用显式名避开撞名）。
func TestAddManyRepoRootSkillSubPathDot(t *testing.T) {
	rootSkillRepo(t, "owner/rootdotmany", "---\nname: dot-many\n---\n\n# 技能\n")
	c := newTestConfig(t)

	results := c.AddMany("owner/rootdotmany", "", []AddSpec{{SubPath: "."}, {SubPath: "", Name: "dot-many-copy"}})
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("第 %d 项应装上（SubPath=%q）: %v", i, r.SubPath, r.Err)
		}
	}
	for _, name := range []string{"dot-many", "dot-many-copy"} {
		if !fileExists(filepath.Join(c.SkillPath(name), "SKILL.md")) {
			t.Errorf("%s 应已装上", name)
		}
	}
}
