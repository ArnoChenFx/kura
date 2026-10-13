package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
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

// TestLibraryThemePersists 验证外观偏好：缺省跟随系统；SetTheme 写进
// config.json 且重新读取得到；添加/移除根目录不会冲掉它；非法值被拒绝且不
// 写盘；文件里被手改出的未知值按跟随系统处理。
func TestLibraryThemePersists(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, err := LoadLibrary(base)
	if err != nil || lib.Theme != ThemeSystem {
		t.Fatalf("缺省应跟随系统，得到 %q, %v", lib.Theme, err)
	}
	if err := lib.SetTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if lib.Theme != ThemeDark {
		t.Errorf("SetTheme 后 Theme = %q", lib.Theme)
	}
	again, _ := LoadLibrary(base)
	if again.Theme != ThemeDark {
		t.Fatalf("重新读取后 Theme = %q，想要 dark", again.Theme)
	}

	// 改根目录列表不能冲掉外观偏好。
	if err := again.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := again.RemoveRoot(extra); err != nil {
		t.Fatal(err)
	}
	if l3, _ := LoadLibrary(base); l3.Theme != ThemeDark {
		t.Errorf("添加/移除根目录后 Theme = %q，想要 dark", l3.Theme)
	}

	// 非法值：报错、不改文件。
	if err := again.SetTheme("sepia"); !errors.Is(err, ErrUnknownTheme) {
		t.Errorf("sepia 应返回 ErrUnknownTheme，得到 %v", err)
	}
	if l4, _ := LoadLibrary(base); l4.Theme != ThemeDark {
		t.Errorf("被拒绝的值不应写盘，Theme = %q", l4.Theme)
	}

	// 空串就是跟随系统。
	if err := again.SetTheme(""); err != nil {
		t.Fatal(err)
	}
	if l5, _ := LoadLibrary(base); l5.Theme != ThemeSystem {
		t.Errorf("空串应存成跟随系统，Theme = %q", l5.Theme)
	}

	// 手改出的未知值：当作跟随系统，不算错误。
	if err := os.WriteFile(base.ConfigPath(), []byte(`{"roots":[],"theme":"sepia"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if l6, err := LoadLibrary(base); err != nil || l6.Theme != ThemeSystem {
		t.Errorf("未知外观应按跟随系统处理，得到 %q, %v", l6.Theme, err)
	}
}

// TestSetThemeOverwritesCorruptConfig 验证 config.json 损坏时 SetTheme 仍能
// 写下偏好（与添加目录一样覆盖坏文件），默认根目录不受影响。
func TestSetThemeOverwritesCorruptConfig(t *testing.T) {
	base, _ := newLibraryFixture(t)
	if err := os.MkdirAll(base.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base.ConfigPath(), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, _ := LoadLibrary(base)
	if err := lib.SetTheme(ThemeLight); err != nil {
		t.Fatalf("损坏后仍应能保存外观: %v", err)
	}
	again, err := LoadLibrary(base)
	if err != nil || again.Theme != ThemeLight || again.Default() != base.Root {
		t.Errorf("覆盖后重新读取得到 %+v, %v", again, err)
	}
}

// TestConfigWritersDoNotClobberEachOther 验证外观与目录列表这两个各自读—改
// —写 config.json 的后台任务同时进行时，谁也不会丢掉对方的改动。每一轮让两
// 个任务同时起跑，轮末检查两边的改动都在；没有 configMu 时会因为互相覆盖
// （或读到写了一半的文件而当作损坏覆盖）很快失败。
func TestConfigWritersDoNotClobberEachOther(t *testing.T) {
	base, extra := newLibraryFixture(t)
	themeLib, _ := LoadLibrary(base)
	rootsLib, _ := LoadLibrary(base)
	for i := 0; i < 300; i++ {
		mode, add := ThemeLight, i%2 == 0
		if i%2 == 1 {
			mode = ThemeDark
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := themeLib.SetTheme(mode); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			var err error
			if add {
				err = rootsLib.AddRoot(extra)
			} else {
				err = rootsLib.RemoveRoot(extra)
			}
			if err != nil {
				t.Error(err)
			}
		}()
		close(start)
		wg.Wait()

		got, err := LoadLibrary(base)
		wantRoots := 1
		if add {
			wantRoots = 2
		}
		if err != nil || got.Theme != mode || len(got.Roots) != wantRoots {
			t.Fatalf("第 %d 轮后 config.json 丢了改动：Theme=%q Roots=%v err=%v，想要 %q 与 %d 个目录",
				i, got.Theme, got.Roots, err, mode, wantRoots)
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
