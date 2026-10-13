package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newLibraryFixture 在临时目录里搭默认根目录、数据目录与一个额外文件夹。
func newLibraryFixture(t *testing.T) (base *Config, extra string) {
	t.Helper()
	dir := t.TempDir()
	base, err := NewConfig(filepath.Join(dir, "skills"), filepath.Join(dir, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	extra = filepath.Join(dir, "other")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, extra
}

func TestLibraryDefaultOnly(t *testing.T) {
	base, _ := newLibraryFixture(t)
	lib, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Roots) != 1 || lib.Default() != base.Root {
		t.Fatalf("Roots = %v, 想要只有 %s", lib.Roots, base.Root)
	}
	if c := lib.Config(base.Root); c.Scope != "" || c.DisabledRoot() != base.DisabledRoot() {
		t.Errorf("默认根目录应沿用原有布局，得到 Scope=%q DisabledRoot=%s", c.Scope, c.DisabledRoot())
	}
}

func TestLibraryAddRemovePersists(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := lib.AddRoot(extra); !errors.Is(err, ErrRootExists) {
		t.Errorf("重复添加应返回 ErrRootExists，得到 %v", err)
	}
	if err := lib.AddRoot(filepath.Join(extra, "missing")); !errors.Is(err, ErrNotDir) {
		t.Errorf("不存在的路径应返回 ErrNotDir，得到 %v", err)
	}

	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Roots) != 2 || again.Roots[1] != extra {
		t.Fatalf("重新读取后 Roots = %v", again.Roots)
	}

	if err := again.RemoveRoot(base.Root); !errors.Is(err, ErrDefaultRoot) {
		t.Errorf("移除默认根目录应返回 ErrDefaultRoot，得到 %v", err)
	}
	if err := again.RemoveRoot(filepath.Join(extra, "nope")); !errors.Is(err, ErrUnknownRoot) {
		t.Errorf("移除未知目录应返回 ErrUnknownRoot，得到 %v", err)
	}
	if err := again.RemoveRoot(extra); err != nil {
		t.Fatal(err)
	}
	if !fileExists(extra) {
		t.Error("从列表移除不应删除磁盘上的文件夹")
	}
	last, _ := LoadLibrary(base)
	if len(last.Roots) != 1 {
		t.Errorf("移除后 Roots = %v", last.Roots)
	}
}

// TestLibraryScopesSeparateSameName 验证两个根目录里同名技能的停用互不覆盖。
func TestLibraryScopesSeparateSameName(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	a, b := lib.Config(base.Root), lib.Config(extra)
	if b.Scope == "" || a.DisabledRoot() == b.DisabledRoot() {
		t.Fatalf("追加根目录应有独立的 disabled：%s vs %s", a.DisabledRoot(), b.DisabledRoot())
	}
	for _, c := range []*Config{a, b} {
		if err := os.MkdirAll(c.SkillPath("dup"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.SkillPath("dup"), "SKILL.md"), []byte(c.Root), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Disable("dup"); err != nil {
			t.Fatalf("停用 %s 下的 dup 失败: %v", c.Root, err)
		}
	}
	for _, c := range []*Config{a, b} {
		if _, err := c.Enable("dup"); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(c.SkillPath("dup"), "SKILL.md"))
		if err != nil || string(got) != c.Root {
			t.Errorf("%s 下的 dup 内容被串了：%q, %v", c.Root, got, err)
		}
	}
}

// TestImportMetaDefaultRootOnly 验证：.skill-lock.json 只描述默认根目录，
// 对追加的根目录 ImportMeta 返回 ErrImportDefaultOnly，且不写任何元数据——
// 即使旁边恰好有一份能匹配上的 lock 文件。
func TestImportMetaDefaultRootOnly(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	def, add := lib.Config(base.Root), lib.Config(extra)
	if !def.CanImportMeta() || add.CanImportMeta() {
		t.Fatalf("CanImportMeta: 默认=%v 追加=%v，想要 true/false", def.CanImportMeta(), add.CanImportMeta())
	}
	writeSkill(t, extra, "locked", nil)
	lock := `{"version":3,"skills":{"locked":{"source":"o/r","sourceType":"github","sourceUrl":"https://github.com/o/r.git","skillPath":"skills/locked/SKILL.md"}}}`
	lockPath := filepath.Join(filepath.Dir(extra), ".skill-lock.json")
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", lockPath} {
		if ids, err := add.ImportMeta(path); !errors.Is(err, ErrImportDefaultOnly) || ids != nil {
			t.Errorf("ImportMeta(%q) = %v, %v，想要 ErrImportDefaultOnly", path, ids, err)
		}
	}
	if fileExists(filepath.Join(add.SkillPath("locked"), MetaFileName)) {
		t.Error("被拒绝的导入不能写元数据")
	}
	// 默认根目录照常导入。
	writeSkill(t, base.Root, "locked", nil)
	if ids, err := def.ImportMeta(lockPath); err != nil || len(ids) != 1 {
		t.Errorf("默认根目录应能导入，得到 %v, %v", ids, err)
	}
}

// TestLibraryCorruptConfigStaysUsable 验证 config.json 损坏时仍有默认根目录。
func TestLibraryCorruptConfigStaysUsable(t *testing.T) {
	base, extra := newLibraryFixture(t)
	if err := os.MkdirAll(base.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base.ConfigPath(), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := LoadLibrary(base)
	if err == nil {
		t.Error("损坏的 config.json 应返回错误")
	}
	if lib == nil || lib.Default() != base.Root {
		t.Fatalf("损坏时仍应返回默认根目录，得到 %+v", lib)
	}
	if err := lib.AddRoot(extra); err != nil {
		t.Fatalf("损坏后仍应能添加目录（覆盖坏文件）: %v", err)
	}
}

func TestReadDescription(t *testing.T) {
	cases := map[string]string{
		"---\nname: a\ndescription: 单行说明\n---\n":                      "单行说明",
		"---\ndescription: \"带引号: 的值\"\n---\n":                        "带引号: 的值",
		"---\nname: a\ndescription: >\n  第一行\n  第二行\nother: x\n---\n": "第一行 第二行",
		"---\r\nname: a\r\ndescription: crlf\r\n---\r\n":              "crlf",
		"---\nmeta:\n  description: nested\n---\n":                    "",
		"# 没有 frontmatter\ndescription: no\n":                         "",
	}
	for body, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readDescription(dir); got != want {
			t.Errorf("readDescription(%q) = %q, 想要 %q", body, got, want)
		}
	}
	if got := readDescription(t.TempDir()); got != "" {
		t.Errorf("缺少 SKILL.md 应返回空串，得到 %q", got)
	}
}
