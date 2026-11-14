package ui

// 技能区排序（按名称 / 按修改时间，新的在前或旧的在前）的界面测试。
//
// 排序偏好存在 ~/.kura/config.json 的 sort，默认按修改时间、新的在前；排序只在 visible()
// 里做一次，网格与列表共用它的结果。排序开关是两个分段（按名称 / 按修改时间），再点一次已选中的
// 「按修改时间」反转方向。mtime 一律用 os.Chtimes 在真实文件系统上造；所有目录都在 t.TempDir()
// 里，绝不触碰真实的 ~/.agents/skills 与 ~/.kura。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// pinNameOrder 把 a 的排序偏好固定成按名称。很多既有用例写于「技能列表固定按名称排序」的年代，
// 它们测的是布局、滚动、选中，只是顺带依赖了「按名字排第一的技能就是第一张卡」；默认改成按修改
// 时间之后，它们用这一行恢复当年的顺序，断言本身一字未改。排序本身的行为由本文件里的用例覆盖。
func pinNameOrder(a *App) { a.sortPref = store.SortPref{Key: store.SortName} }

// sortBase 是造 mtime 的基准（整秒，远早于「现在」）：各技能在它之上加整小时，彼此可区分。
var sortBase = time.Date(2024, 3, 5, 6, 7, 8, 0, time.UTC)

// 三个固定的偏好值，测试里一律写字面量，不借 store.DefaultSort() 自己跟自己比。
var (
	prefName   = store.SortPref{Key: "name"}
	prefNewest = store.SortPref{Key: "time", Desc: true}
	prefOldest = store.SortPref{Key: "time", Desc: false}
)

// 四个技能 alpha/bravo/charlie/delta 的三种顺序，两两不同（所以「恒等的比较」「忽略偏好的缓存」
// 都逃不过）：mtime 依次是 alpha=20h、bravo=40h、charlie=10h、delta=30h。
var (
	sortByName = []string{"alpha", "bravo", "charlie", "delta"}
	sortNewest = []string{"bravo", "delta", "alpha", "charlie"}
	sortOldest = []string{"charlie", "alpha", "delta", "bravo"}
)

// seedTimed 造一个技能，并把它的 mtime（SKILL.md 与目录自身）设成基准之后 hours 小时。
// 目录要最后设：往里写文件会把目录的 mtime 改回「现在」。
func seedTimed(t *testing.T, cfg *store.Config, name string, hours int) {
	t.Helper()
	seedDescribed(t, cfg, name, "Description of "+name, nil)
	tm := sortBase.Add(time.Duration(hours) * time.Hour)
	dir := cfg.SkillPath(name)
	for _, p := range []string{filepath.Join(dir, "SKILL.md"), dir} {
		if err := os.Chtimes(p, tm, tm); err != nil {
			t.Fatalf("设置 mtime 失败 %s: %v", p, err)
		}
	}
}

// seedSortFixture 造 alpha/bravo/charlie/delta 四个技能（mtime 见上）。
func seedSortFixture(t *testing.T, cfg *store.Config) {
	t.Helper()
	for name, hours := range map[string]int{"alpha": 20, "bravo": 40, "charlie": 10, "delta": 30} {
		seedTimed(t, cfg, name, hours)
	}
}

// visibleNames 是 a.visible() 里的技能名，按显示顺序。
func visibleNames(a *App) []string {
	var out []string
	for _, s := range a.visible() {
		out = append(out, s.Name)
	}
	return out
}

// onScreenOrder 把 want 里上一帧能找到的技能名按屏幕位置排序（先上下、后左右；同一行的卡片
// 名字 Y 相同），也就是用户实际看到的顺序。网格与列表都适用。
func onScreenOrder(tt *myui.Tester, want []string) []string {
	type spot struct {
		name string
		r    myui.Rect
	}
	var found []spot
	for _, n := range want {
		if r, ok := tt.Find(n); ok && r.W > 0 && r.H > 0 {
			found = append(found, spot{n, r})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i].r, found[j].r
		if d := a.Y - b.Y; d > 4 || d < -4 { // 不同行
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.name
	}
	return out
}

// savedSort 读回 config.json 里的排序偏好。
func savedSort(t *testing.T, cfg *store.Config) store.SortPref {
	t.Helper()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return lib.Sort
}

// skillStates 把技能列表写成「状态:名字」，便于在失败信息里看清是哪个副本。
func skillStates(skills []*store.Skill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = string(s.State) + ":" + s.Name
	}
	return out
}

// TestDefaultOrderIsNewestFirst 钉住默认：没存过排序时，技能按修改时间、新的在前，
// 屏幕上看到的也是这个顺序（而不是名称顺序）。
func TestDefaultOrderIsNewestFirst(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	if a.sortPref != prefNewest {
		t.Fatalf("默认排序偏好 = %+v，想要 %+v（按修改时间、新的在前）", a.sortPref, prefNewest)
	}
	if got := visibleNames(a); !slices.Equal(got, sortNewest) {
		t.Errorf("默认的 visible() 顺序 = %v，想要新的在前 %v", got, sortNewest)
	}
	if got := onScreenOrder(tt, sortByName); !slices.Equal(got, sortNewest) {
		t.Errorf("默认的屏幕顺序 = %v，想要新的在前 %v", got, sortNewest)
	}
}

// TestVisibleOrdersByPreference 验证三种偏好各自的顺序。偏好是直接赋给 a.sortPref 的、不经
// 任何手动清缓存的路径：能换序，说明排序偏好本身就在 visible() 的缓存键里（AGENTS.md：
// 排序偏好变化要作废 visible() 的缓存）。
func TestVisibleOrdersByPreference(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	showApp(a, 1280, 800) // 完成首次扫描
	for _, step := range []struct {
		name string
		pref store.SortPref
		want []string
	}{
		{"按名称", prefName, sortByName},
		{"新的在前", prefNewest, sortNewest},
		{"旧的在前", prefOldest, sortOldest},
		{"回到按名称", prefName, sortByName},
		{"零值等同默认", store.SortPref{}, sortNewest},
	} {
		a.sortPref = step.pref
		if got := visibleNames(a); !slices.Equal(got, step.want) {
			t.Errorf("%s：顺序 = %v，想要 %v", step.name, got, step.want)
		}
	}

	// 输入与偏好都没变时命中缓存：同一个切片，不重排。
	first := a.visible()
	if again := a.visible(); len(first) == 0 || &first[0] != &again[0] {
		t.Error("输入与偏好都没变时应命中缓存，返回同一个切片")
	}
}

// TestTimeSortTiesBreakByNameInBothDirections 验证时间相同再按名称，且两个方向下同时间的条目都
// 按名称升序（不随方向翻转）；名称比较不区分大小写、同名再按原名；零值时间视为最旧。技能全部
// 是内存里造的、Dir 为空：证明排序只看扫描时放进 Skill 的 ModTime，不读磁盘。
func TestTimeSortTiesBreakByNameInBothDirections(t *testing.T) {
	same, older, newer := sortBase.Add(5*time.Hour), sortBase, sortBase.Add(9*time.Hour)
	a := &App{skills: []*store.Skill{
		{Name: "echo", ModTime: same, State: store.StateEnabled},
		{Name: "alpha", ModTime: same, State: store.StateEnabled},
		{Name: "bravo", ModTime: newer, State: store.StateEnabled},
		{Name: "Alpha", ModTime: same, State: store.StateEnabled},
		{Name: "zeta", ModTime: older, State: store.StateEnabled},
		{Name: "unknown", State: store.StateEnabled}, // 零值时间
	}}
	for _, c := range []struct {
		pref store.SortPref
		want []string
	}{
		{prefNewest, []string{"bravo", "Alpha", "alpha", "echo", "zeta", "unknown"}},
		{prefOldest, []string{"unknown", "zeta", "Alpha", "alpha", "echo", "bravo"}},
		{prefName, []string{"Alpha", "alpha", "bravo", "echo", "unknown", "zeta"}},
	} {
		a.sortPref = c.pref
		if got := visibleNames(a); !slices.Equal(got, c.want) {
			t.Errorf("%+v：顺序 = %v，想要 %v", c.pref, got, c.want)
		}
	}
}

// TestVisibleOrderIsAPureFunctionOfInputAndPreference 验证「稳定可复现」：同一份输入加同一个
// 偏好，不管之前切换过几次、怎么切换，结果都一样；同名的启用/停用/回收站副本（比较结果为 0）
// 保持它们在扫描原序里的相对顺序。
func TestVisibleOrderIsAPureFunctionOfInputAndPreference(t *testing.T) {
	tm := sortBase.Add(3 * time.Hour)
	enabled := &store.Skill{Name: "dup", State: store.StateEnabled, ModTime: tm}
	disabled := &store.Skill{Name: "dup", State: store.StateDisabled, ModTime: tm}
	trashed := &store.Skill{Name: "dup", State: store.StateTrashed, ModTime: tm}
	other := &store.Skill{Name: "other", State: store.StateEnabled, ModTime: tm.Add(time.Hour)}
	a := &App{skills: []*store.Skill{enabled, disabled, trashed, other}}

	// 三个副本同名同时间：任何偏好下它们都保持扫描原序（「全部」不含回收站，所以是启用、停用）。
	for _, pref := range []store.SortPref{prefName, prefNewest, prefOldest} {
		a.sortPref = pref
		var dups []*store.Skill
		for _, s := range a.visible() {
			if s.Name == "dup" {
				dups = append(dups, s)
			}
		}
		if !slices.Equal(dups, []*store.Skill{enabled, disabled}) {
			t.Errorf("%+v：同名副本的相对顺序被打乱：%v", pref, skillStates(dups))
		}
	}

	// 来回切换后回到原偏好：结果与第一次完全一致。
	a.sortPref = prefOldest
	before := slices.Clone(a.visible())
	for _, pref := range []store.SortPref{prefName, prefNewest, prefName, prefOldest, prefNewest, prefOldest} {
		a.sortPref = pref
		a.visible()
	}
	if after := a.visible(); !slices.Equal(before, after) {
		t.Errorf("来回切换后同一偏好的结果变了：%v → %v", skillStates(before), skillStates(after))
	}
}

// TestSetSortAsync 验证 SetSortAsync：sortPref 立刻变、状态行无错误、后台写盘；重复调用幂等；
// 连续切换只有最新一次写盘；未知值被拒绝并在状态行报错；重启后仍是所选。
func TestSetSortAsync(t *testing.T) {
	a, cfg := newTestApp(t)
	var queue []func()
	a.spawn = func(fn func()) { queue = append(queue, fn) }

	a.SetSortAsync(prefName)
	if a.sortPref != prefName {
		t.Fatalf("sortPref 应立刻变成按名称，得到 %+v", a.sortPref)
	}
	if a.statusErr {
		t.Errorf("状态行不应有错误：%q", a.status)
	}
	if len(queue) != 1 {
		t.Fatalf("应排一个后台写盘任务，得到 %d", len(queue))
	}
	if got := savedSort(t, cfg); got != prefNewest {
		t.Errorf("任务排队期间不应已写盘，config.json 里的排序 = %+v", got)
	}
	a.SetSortAsync(prefName) // 幂等
	if len(queue) != 1 {
		t.Errorf("重复设置同一偏好不应再排任务，得到 %d", len(queue))
	}
	for _, fn := range queue {
		fn()
	}
	if got := savedSort(t, cfg); got != prefName {
		t.Errorf("config.json 里的排序 = %+v，想要按名称", got)
	}
	if b := newAppFor(cfg); b.sortPref != prefName {
		t.Errorf("重启后 sortPref = %+v，想要按名称", b.sortPref)
	}

	// 连续切换：旧任务发现序号已变就什么也不写，最终落盘的是最新一次。
	queue = nil
	a.SetSortAsync(prefOldest)
	a.SetSortAsync(prefNewest)
	a.SetSortAsync(prefName)
	a.SetSortAsync(prefOldest)
	for _, fn := range queue {
		fn()
	}
	if got := savedSort(t, cfg); got != prefOldest {
		t.Errorf("连续切换后 config.json 里的排序 = %+v，想要最新一次（旧的在前）", got)
	}

	// 未知值：不换，状态行报错。
	a.SetSortAsync(store.SortPref{Key: "size"})
	if a.sortPref != prefOldest || !a.statusErr {
		t.Errorf("未知排序应被拒绝：sortPref=%+v statusErr=%v", a.sortPref, a.statusErr)
	}
}

// TestSortPrefRidesAlongWhenConfigIsCorrupt 验证 libCopy 带着排序偏好：config.json 损坏时，别的
// 偏好（这里是外观）的写入会覆盖坏文件，本次运行里已定下的排序要随内存副本一起写回去，不能丢。
func TestSortPrefRidesAlongWhenConfigIsCorrupt(t *testing.T) {
	a, cfg := newTestApp(t)
	a.SetSortAsync(prefName) // 同步 spawn：立刻写盘
	if err := os.WriteFile(cfg.ConfigPath(), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.SetThemeAsync(store.ThemeDark)
	lib, err := store.LoadLibrary(cfg)
	if err != nil || lib.Sort != prefName || lib.Theme != store.ThemeDark {
		t.Errorf("覆盖坏文件后：Sort=%+v Theme=%q err=%v，想要按名称与 dark", lib.Sort, lib.Theme, err)
	}
}

// TestSortSwitchReordersGridAndList 是端到端：点排序开关，网格与列表上用户实际看到的顺序都立刻变成
// 对应的顺序（两种排列永远是同一个顺序），偏好随之写进 config.json。点序列：按名称 → 按修改时间
// （新选中，新的在前）→ 再点一次（反转成旧的在前）→ 再点一次（反转回新的在前）→ 按名称。
func TestSortSwitchReordersGridAndList(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		a.SetViewAsync(mode)
		refresh(a, tt)
		for _, step := range []struct {
			label string // 点之前这一段的无障碍名
			pref  store.SortPref
			want  []string
		}{
			{locales.UiSortName(), prefName, sortByName},
			{locales.UiSortTimeNewest(), prefNewest, sortNewest}, // 当前是按名称：新选中，从新的在前开始
			{locales.UiSortTimeNewest(), prefOldest, sortOldest}, // 已选中再点：反转
			{locales.UiSortTimeOldest(), prefNewest, sortNewest}, // 反转回来（此时它的名字是「旧的在前」）
			{locales.UiSortName(), prefName, sortByName},
		} {
			click(t, a, tt, step.label)
			refresh(a, tt)
			if a.sortPref != step.pref {
				t.Fatalf("%s/点 %q 后 sortPref = %+v，想要 %+v", mode, step.label, a.sortPref, step.pref)
			}
			if got := onScreenOrder(tt, sortByName); !slices.Equal(got, step.want) {
				t.Errorf("%s/点 %q 后屏幕顺序 = %v，想要 %v", mode, step.label, got, step.want)
			}
			if got := savedSort(t, cfg); got != step.pref {
				t.Errorf("%s/点 %q 后 config.json 里的排序 = %+v，想要 %+v", mode, step.label, got, step.pref)
			}
		}
	}
}

// TestSortSwitchIsKeyboardOperable 验证排序开关能只用键盘操作，包括反转方向：焦点在开关上时，方向键
// 在「按名称 / 按修改时间」两段之间选择；焦点在已选中的「按修改时间」上按空格或回车就是再点一次，
// 反转方向。（MyGo 里已选中的单选分段不报 Changed，但空格、回车与指针再点一样报 Clicked。）
func TestSortSwitchIsKeyboardOperable(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiSortName()) // 点按名称：选中它，同时把焦点放上去
	if a.sortPref != prefName {
		t.Fatalf("前置：点按名称后 sortPref = %+v", a.sortPref)
	}
	for _, step := range []struct {
		what string
		key  myui.Key
		pref store.SortPref
		want []string
	}{
		{"→ 选到按修改时间（新选中，新的在前）", myui.KeyRight, prefNewest, sortNewest},
		{"空格 反转为旧的在前", myui.KeySpace, prefOldest, sortOldest},
		{"回车 反转回新的在前", myui.KeyEnter, prefNewest, sortNewest},
		{"空格 再反转为旧的在前", myui.KeySpace, prefOldest, sortOldest},
		{"← 选回按名称", myui.KeyLeft, prefName, sortByName},
		{"在按名称上按空格：什么也不变", myui.KeySpace, prefName, sortByName},
		{"→ 再选按修改时间：重新从新的在前开始", myui.KeyRight, prefNewest, sortNewest},
	} {
		tt.Key(0, step.key)
		refresh(a, tt)
		refresh(a, tt)
		if a.sortPref != step.pref {
			t.Fatalf("%s：sortPref = %+v，想要 %+v", step.what, a.sortPref, step.pref)
		}
		if got := onScreenOrder(tt, sortByName); !slices.Equal(got, step.want) {
			t.Errorf("%s：屏幕顺序 = %v，想要 %v", step.what, got, step.want)
		}
	}
	if got := savedSort(t, cfg); got != prefNewest {
		t.Errorf("键盘操作也应写盘：config.json 里的排序 = %+v，想要新的在前", got)
	}
}

// TestSortSwitchKeepsSelection 验证切换排序不丢选中项：选中的技能仍是原来那个（以技能名与存放
// 状态为准），右栏详情还在，网格用的选择下标跟着它换到新位置。网格与列表都测。
func TestSortSwitchKeepsSelection(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			seedSortFixture(t, cfg)
			a.viewMode = mode
			tt := showApp(a, 1280, 800)
			click(t, a, tt, "charlie")
			if a.selName != "charlie" || !tt.HasText("关闭详情") {
				t.Fatalf("前置：点 charlie 应选中并出现详情，selName=%q", a.selName)
			}
			seq := a.selSeq
			// 点序列：按名称 → 按修改时间（新选中）→ 再点（旧的在前）→ 再点（新的在前）→ 按名称。
			for _, label := range []string{
				locales.UiSortName(), locales.UiSortTimeNewest(), locales.UiSortTimeNewest(),
				locales.UiSortTimeOldest(), locales.UiSortName(),
			} {
				click(t, a, tt, label)
				refresh(a, tt)
				if a.selName != "charlie" || a.selState != store.StateEnabled {
					t.Fatalf("点 %q 后选中项变了：selName=%q selState=%q", label, a.selName, a.selState)
				}
				if !tt.HasText("关闭详情") {
					t.Errorf("点 %q 后右栏详情不见了", label)
				}
				if want := slices.Index(visibleNames(a), "charlie"); a.selIdx != want {
					t.Errorf("点 %q 后选择下标 = %d，应跟随 charlie 到新位置 %d", label, a.selIdx, want)
				}
			}
			if a.selSeq != seq {
				t.Errorf("换序不是用户改选，不该推进选择代数：%d → %d", seq, a.selSeq)
			}
		})
	}
}

// TestSortSwitchKeepsSelectionOfSameNameCopies 验证同名的启用/停用副本并存时，换序后选中的仍是
// 用户选的那一个副本（选中以「名字 + 存放状态」为准，不是名字）。
func TestSortSwitchKeepsSelectionOfSameNameCopies(t *testing.T) {
	a, cfg := newTestApp(t)
	seedTimed(t, cfg, "dup", 5)
	if _, err := cfg.Disable("dup"); err != nil { // 停用的旧副本（mtime 较旧）
		t.Fatal(err)
	}
	seedTimed(t, cfg, "dup", 50) // 重新安装的启用副本（mtime 较新）
	seedTimed(t, cfg, "other", 20)
	tt := showApp(a, 1280, 800)
	a.setSelection("dup", store.StateDisabled)
	refresh(a, tt)
	for _, pref := range []store.SortPref{prefName, prefOldest, prefNewest} {
		a.SetSortAsync(pref)
		refresh(a, tt)
		refresh(a, tt)
		if a.selName != "dup" || a.selState != store.StateDisabled {
			t.Fatalf("%+v：选中的应仍是停用的 dup，得到 %q/%q", pref, a.selName, a.selState)
		}
		sel := a.selected()
		if sel == nil || sel.State != store.StateDisabled {
			t.Fatalf("%+v：详情应仍是停用的副本，得到 %+v", pref, sel)
		}
		if want := slices.IndexFunc(a.visible(), func(s *store.Skill) bool { return a.isSelected(s) }); a.selIdx != want {
			t.Errorf("%+v：选择下标 = %d，应指向停用的副本 %d", pref, a.selIdx, want)
		}
	}
}

// TestSortSaveFailureIsReported 验证写不进 config.json 时：顺序照样换了（不回滚），状态行报告没
// 存上，界面继续可用。
func TestSortSaveFailureIsReported(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	if err := os.MkdirAll(cfg.ConfigPath(), 0o755); err != nil { // 让 config.json 的位置被文件夹占着
		t.Fatal(err)
	}
	click(t, a, tt, locales.UiSortName())
	refresh(a, tt)
	if a.sortPref != prefName {
		t.Errorf("存盘失败也不应回滚排序，sortPref=%+v", a.sortPref)
	}
	if got := onScreenOrder(tt, sortByName); !slices.Equal(got, sortByName) {
		t.Errorf("存盘失败后屏幕顺序应已是按名称，得到 %v", got)
	}
	if !a.statusErr || !tt.HasText("排序方式没能保存") {
		t.Errorf("状态行应报告没存上，statusErr=%v 文本 %v", a.statusErr, tt.Texts())
	}
}

// TestSortPreferenceSurvivesRestart 验证选择重启后仍然生效：点完开关，从磁盘重新建一个应用
// （模拟重启），它的偏好与第一帧的顺序都是上次选的。
func TestSortPreferenceSurvivesRestart(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	for _, step := range []struct {
		label string
		pref  store.SortPref
		want  []string
	}{
		{locales.UiSortTimeNewest(), prefOldest, sortOldest}, // 默认是新的在前：再点反转成旧的在前
		{locales.UiSortName(), prefName, sortByName},
		{locales.UiSortTimeNewest(), prefNewest, sortNewest}, // 当前是按名称：新选中，新的在前
	} {
		click(t, a, tt, step.label)
		refresh(a, tt)
		b := newAppFor(cfg) // 重启：从磁盘重新读
		if b.sortPref != step.pref {
			t.Fatalf("选 %q 后重启，sortPref = %+v，想要 %+v", step.label, b.sortPref, step.pref)
		}
		bt := showApp(b, 1280, 800)
		if got := onScreenOrder(bt, sortByName); !slices.Equal(got, step.want) {
			t.Errorf("选 %q 后重启，第一帧顺序 = %v，想要 %v", step.label, got, step.want)
		}
	}
}

// TestBrokenSortInConfigFallsBackToDefault 验证手改出的未知/损坏的 sort 退回默认（按修改时间、
// 新的在前），而且不报错：libErr 为空，根目录列表等其余内容照常读出。
func TestBrokenSortInConfigFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{`"by-vibes"`, `7`, `{"key":"size"}`, `{"key":"time","desc":"maybe"}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			_, cfg := newTestApp(t)
			seedSortFixture(t, cfg)
			if err := store.SaveHomeConfig(cfg, &store.HomeConfig{Sort: json.RawMessage(raw)}); err != nil {
				t.Fatal(err)
			}
			b := newAppFor(cfg)
			if b.libErr != "" || b.sortPref != prefNewest {
				t.Fatalf("损坏的 sort 应按默认处理且不报错：libErr=%q sortPref=%+v", b.libErr, b.sortPref)
			}
			tt := showApp(b, 1280, 800)
			if got := onScreenOrder(tt, sortByName); !slices.Equal(got, sortNewest) {
				t.Errorf("退回默认后的屏幕顺序 = %v，想要新的在前 %v", got, sortNewest)
			}
		})
	}
}

// TestSortUsesTheScanSnapshotNotTheDisk 验证 ModTime 是扫描时算好放进 Skill 的快照，换序时不再
// 读磁盘：扫描完成后把技能目录整个删掉，换序仍然按扫描时的 ModTime 排。
func TestSortUsesTheScanSnapshotNotTheDisk(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	showApp(a, 1280, 800)
	if err := os.RemoveAll(cfg.Root); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		pref store.SortPref
		want []string
	}{{prefOldest, sortOldest}, {prefNewest, sortNewest}, {prefName, sortByName}} {
		a.sortPref = c.pref
		if got := visibleNames(a); !slices.Equal(got, c.want) {
			t.Errorf("磁盘上已没有技能目录，%+v 的顺序 = %v，想要 %v（应沿用扫描时的 ModTime）", c.pref, got, c.want)
		}
	}
}

// TestSortSegments 验证排序开关的分段逻辑：sortIndex 把偏好映射到「按名称 / 按修改时间」两段（零值与
// 未知值按默认，落在按修改时间）；新选中一段总是从新的在前开始；反转只改方向；每个状态的无障碍名、
// 悬停提示与图标都对：只有已选中的「按修改时间」才带「再点一次」的提示；中英文文案都有且互不重复。
func TestSortSegments(t *testing.T) {
	for _, c := range []struct {
		pref store.SortPref
		want int
	}{
		{prefName, sortNameSegment}, {prefNewest, sortTimeSegment}, {prefOldest, sortTimeSegment},
		{store.SortPref{}, sortTimeSegment},                          // 零值等同默认
		{store.SortPref{Key: "size"}, sortTimeSegment},               // 未知值按默认
		{store.SortPref{Key: "name", Desc: true}, sortNameSegment},   // 名称的 desc 没有意义
		{store.SortPref{Key: "weird", Desc: false}, sortTimeSegment}, // 未知值按默认
	} {
		if got := sortIndex(c.pref); got != c.want {
			t.Errorf("sortIndex(%+v) = %d，想要 %d", c.pref, got, c.want)
		}
	}
	if sortSegments != 2 {
		t.Fatalf("排序开关应有 2 个分段，得到 %d", sortSegments)
	}
	if got := sortChosen(sortNameSegment); got != prefName {
		t.Errorf("新选中按名称 = %+v", got)
	}
	if got := sortChosen(sortTimeSegment); got != prefNewest {
		t.Errorf("新选中按修改时间应从新的在前开始，得到 %+v", got)
	}
	if got := sortReversed(prefNewest); got != prefOldest {
		t.Errorf("反转新的在前 = %+v，想要旧的在前", got)
	}
	if got := sortReversed(prefOldest); got != prefNewest {
		t.Errorf("反转旧的在前 = %+v，想要新的在前", got)
	}

	t.Cleanup(func() { i18n.Set(i18n.ZH) })
	for _, lang := range []i18n.Lang{i18n.ZH, i18n.EN} {
		i18n.Set(lang)
		type look struct{ label, tip string }
		looks := map[string]look{}
		for _, c := range []struct {
			name      string
			segment   int
			cur       store.SortPref
			wantLabel string
			wantTip   string
		}{
			{"名称（已选中）", sortNameSegment, prefName, locales.UiSortName(), locales.UiSortName()},
			{"名称（未选中）", sortNameSegment, prefNewest, locales.UiSortName(), locales.UiSortName()},
			{"时间·新的在前（已选中）", sortTimeSegment, prefNewest, locales.UiSortTimeNewest(), locales.UiSortTimeNewestTip()},
			{"时间·旧的在前（已选中）", sortTimeSegment, prefOldest, locales.UiSortTimeOldest(), locales.UiSortTimeOldestTip()},
			// 当前是按名称：时间这一段显示点它之后会得到的方向（新的在前），提示里没有「再点一次」。
			{"时间（未选中）", sortTimeSegment, prefName, locales.UiSortTimeNewest(), locales.UiSortTimeNewest()},
		} {
			label, tip, icon := sortSegmentLook(c.segment, c.cur)
			if label != c.wantLabel || tip != c.wantTip || icon == nil {
				t.Errorf("%s/%s：look = %q / %q，想要 %q / %q", lang, c.name, label, tip, c.wantLabel, c.wantTip)
			}
			looks[c.name] = look{label, tip}
		}
		for _, name := range []string{"时间·新的在前（已选中）", "时间·旧的在前（已选中）"} {
			if l := looks[name]; l.tip == l.label || l.tip == "" {
				t.Errorf("%s/%s：已选中的「按修改时间」悬停提示应比无障碍名多一句反转说明，得到 %q / %q", lang, name, l.label, l.tip)
			}
		}
		x, y, z := looks["名称（已选中）"].label, looks["时间·新的在前（已选中）"].label, looks["时间·旧的在前（已选中）"].label
		if x == y || y == z || x == z {
			t.Errorf("%s：三种状态的无障碍名应互不相同：%q %q %q", lang, x, y, z)
		}
	}
	i18n.Set(i18n.ZH)

	// 图标：名称、新的在前、旧的在前三种状态各用各的。
	_, _, nameIcon := sortSegmentLook(sortNameSegment, prefName)
	_, _, newIcon := sortSegmentLook(sortTimeSegment, prefNewest)
	_, _, oldIcon := sortSegmentLook(sortTimeSegment, prefOldest)
	if nameIcon == newIcon || newIcon == oldIcon || nameIcon == oldIcon {
		t.Error("三种状态应使用三个不同的图标")
	}
}

// TestSortSwitchIsNotSquashedInNarrowFilterBar 验证窄窗口里筛选栏换行时：排序开关不被挤扁（两个分段
// 在各个宽度下大小与最宽时完全一样，Shrink(0)），搜索框也没被挤坏；从左到右依次是 搜索框 → 排序 →
// 排列，同在一行、互不重叠，仍完整落在筛选栏内。宽度覆盖：用户把两侧栏拖到最宽时的最窄中栏（300）、
// 默认窗口最小宽度（880）且详情栏打开时的中栏（334），以及更宽的几个。
//
// 「搜索框没被挤坏」：从 334 起，搜索框的占位文字（输入框自己的最小宽度）连同搜索框右内边距、与排序
// 开关的间距，完整落在排序开关左边。300 是极端情形（输入框的最小宽度本来就放不下），只要求占位文字
// 不盖到排序开关下面。
func TestSortSwitchIsNotSquashedInNarrowFilterBar(t *testing.T) {
	const searchBoxTail = 20 // 搜索框右内边距 12 + 与排序开关的间距 8（DIP）
	labels := []string{locales.UiSortName(), locales.UiSortTimeNewest()}
	measure := func(width int) (sortRects []myui.Rect, search, viewList myui.Rect) {
		a, _ := newTestApp(t)
		tt := myui.NewTester(func(c *myui.Context) {
			a.skin = kuraScheme.apply(c, store.ThemeLight)
			a.pal = a.skin.pal
			a.viewFilterBar(c)
		}, width, 240)
		tt.SetPreferences(myui.Preferences{ReduceMotion: true})
		tt.Frame()
		for _, l := range labels {
			r, ok := tt.Find(l)
			if !ok {
				t.Fatalf("宽 %d：找不到排序分段 %q，文本 %v", width, l, tt.Texts())
			}
			sortRects = append(sortRects, r)
		}
		var ok bool
		if search, ok = tt.Find(locales.UiSearchPlaceholder()); !ok {
			t.Fatalf("宽 %d：找不到搜索框", width)
		}
		if viewList, ok = tt.Find(locales.UiViewList()); !ok {
			t.Fatalf("宽 %d：找不到排列开关", width)
		}
		return
	}
	wide, _, _ := measure(1000)
	for i, r := range wide {
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("最宽时分段 %d 就没有大小：%v", i, r)
		}
	}
	for _, width := range []int{300, 334, 360, 420, 560, 880} {
		rects, search, view := measure(width)
		for i, r := range rects {
			if r.W != wide[i].W || r.H != wide[i].H {
				t.Errorf("宽 %d：分段 %d 被挤变形：%gx%g，最宽时是 %gx%g", width, i, r.W, r.H, wide[i].W, wide[i].H)
			}
			if r.X < 20 || r.X+r.W > float32(width)-20+0.5 {
				t.Errorf("宽 %d：分段 %d 超出筛选栏：%v", width, i, r)
			}
			if i > 0 && r.X < rects[i-1].X+rects[i-1].W-0.5 {
				t.Errorf("宽 %d：分段 %d 与前一个分段重叠", width, i)
			}
			if r.Y+r.H < search.Y || search.Y+search.H < r.Y {
				t.Errorf("宽 %d：排序分段 %d 应与搜索框同一行：search=%v 分段=%v", width, i, search, r)
			}
		}
		last := rects[len(rects)-1]
		if !(search.X < rects[0].X && last.X+last.W <= view.X+0.5) {
			t.Errorf("宽 %d：顺序应是 搜索框 → 排序 → 排列：search.X=%g 排序=[%g,%g] 排列.X=%g",
				width, search.X, rects[0].X, last.X+last.W, view.X)
		}
		need := float32(0)
		if width >= 334 {
			need = searchBoxTail
		}
		if gap := rects[0].X - (search.X + search.W); gap < need-0.5 {
			t.Errorf("宽 %d：搜索框被挤坏：占位文字 [%g,%g] 与排序开关（从 %g 起）之间只有 %g，至少要 %g",
				width, search.X, search.X+search.W, rects[0].X, gap, need)
		}
	}
}

// TestSortSwitchTooltipsExplainTheFlip 验证悬停提示：已选中的「按修改时间」说明可以再点一次反转方向，
// 并且提示随当前方向变。
func TestSortSwitchTooltipsExplainTheFlip(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	hover := func(label string) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("找不到 %q", label)
		}
		// 先把指针移开再移回来：点击后指针还停在原地，没有真实位移的 Move 不会重新触发悬停提示。
		tt.Move(2, 2)
		tt.Advance(time.Second)
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Advance(time.Second)
	}
	hover(locales.UiSortTimeNewest()) // 默认：新的在前，已选中
	if !tt.HasText(locales.UiSortTimeNewestTip()) {
		t.Errorf("已选中的新的在前应提示可反转，文本 %v", tt.Texts())
	}
	click(t, a, tt, locales.UiSortTimeNewest()) // 反转
	hover(locales.UiSortTimeOldest())
	if !tt.HasText(locales.UiSortTimeOldestTip()) {
		t.Errorf("反转后应提示旧的在前并可再反转，文本 %v", tt.Texts())
	}
}

// TestSortReverseNeedsTheTimeSegment 验证反转只发生在已选中的「按修改时间」上：在按名称上再点什么也
// 不变也不写盘；从按名称点到按修改时间永远是新选中（新的在前），不会因为上一次是旧的在前而沿用。
func TestSortReverseNeedsTheTimeSegment(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSortFixture(t, cfg)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiSortTimeNewest()) // 反转成旧的在前
	if a.sortPref != prefOldest {
		t.Fatalf("前置：再点一次应反转成旧的在前，得到 %+v", a.sortPref)
	}
	click(t, a, tt, locales.UiSortName())
	if a.sortPref != prefName {
		t.Fatalf("前置：点按名称，得到 %+v", a.sortPref)
	}
	var queue []func()
	a.spawn = func(fn func()) { queue = append(queue, fn) }
	click(t, a, tt, locales.UiSortName()) // 在按名称上再点：什么也不该发生
	if a.sortPref != prefName || len(queue) != 0 {
		t.Errorf("在按名称上再点不应改变偏好或排写盘任务：sortPref=%+v 任务=%d", a.sortPref, len(queue))
	}
	click(t, a, tt, locales.UiSortTimeNewest()) // 按名称 → 按修改时间：新选中，新的在前（不沿用上次的旧的在前）
	if a.sortPref != prefNewest {
		t.Errorf("从按名称点到按修改时间应从新的在前开始，得到 %+v", a.sortPref)
	}
}

// TestSortSwitchKeepsViewportSane 验证滚动之后换序，视口停在一个连贯的位置，不会跳回顶部、出现空白
// 或越界：可见的技能始终是当前顺序里连续的一段、个数不变、没有被重置到顶部。两种排列各有 MyGo 自己的
// 锚定方式，这里如实钉住：列表以顶部那一项为锚（视口跟着同一个技能走，下标变了它还在顶部），网格保持
// 偏移不变（同一位置换成新顺序里的技能）；这与过滤、搜索改变行数时的默认行为一致，kura 不另加干预。
func TestSortSwitchKeepsViewportSane(t *testing.T) {
	for _, mode := range []string{store.ViewList, store.ViewGrid} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			names := make([]string, 60)
			for i := range names {
				names[i] = fmt.Sprintf("s-%02d", i)
				seedTimed(t, cfg, names[i], (i*37)%60) // 37 与 60 互质：mtime 各不相同，且与名称顺序错开
			}
			a.viewMode = mode
			tt := showApp(a, 1280, 800)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			refresh(a, tt)
			if mode == store.ViewList {
				a.list.ScrollTo(30, myui.Start)
			} else {
				a.grid.ScrollTo(30, myui.Start)
			}
			refresh(a, tt)
			refresh(a, tt)

			// window 是上一帧可见的技能在当前顺序里的下标（升序）。
			window := func() []int {
				order := visibleNames(a)
				var idx []int
				for _, n := range names {
					if r, ok := tt.Find(n); ok && r.W > 0 && r.H > 0 {
						idx = append(idx, slices.Index(order, n))
					}
				}
				slices.Sort(idx)
				return idx
			}
			contiguous := func(idx []int) bool {
				for i := 1; i < len(idx); i++ {
					if idx[i] != idx[i-1]+1 {
						return false
					}
				}
				return len(idx) > 0
			}
			before := window()
			if !contiguous(before) || before[0] == 0 {
				t.Fatalf("前置：应已滚离顶部且可见的是连续一段，得到 %v", before)
			}
			topBefore := onScreenOrder(tt, names)[0]

			// 点序列：按名称 → 按修改时间（新选中）→ 再点（旧的在前）→ 再点（新的在前）。
			for _, label := range []string{
				locales.UiSortName(), locales.UiSortTimeNewest(), locales.UiSortTimeNewest(), locales.UiSortTimeOldest(),
			} {
				click(t, a, tt, label)
				refresh(a, tt)
				refresh(a, tt)
				win := window()
				if !contiguous(win) {
					t.Errorf("点 %q 后可见的技能不是连续一段：%v", label, win)
					continue
				}
				if len(win) != len(before) {
					t.Errorf("点 %q 后可见个数 %d 与之前的 %d 不同（出现空白或跳变）", label, len(win), len(before))
				}
				if win[0] == 0 {
					t.Errorf("点 %q 后视口被重置到了顶部：%v", label, win)
				}
				if win[len(win)-1] >= len(names) {
					t.Errorf("点 %q 后视口越界：%v", label, win)
				}
				if mode == store.ViewList {
					if top := onScreenOrder(tt, names)[0]; top != topBefore {
						t.Errorf("点 %q 后列表顶部的技能变了：%s → %s（列表应以顶部那一项为锚）", label, topBefore, top)
					}
				} else if win[0] != before[0] {
					t.Errorf("点 %q 后网格的偏移变了：可见下标 %v，之前 %v（网格应保持偏移）", label, win, before)
				}
			}
		})
	}
}

// TestRenderSortShots 把排序开关渲染成 PNG（深色、浅色）供人工看图，并把关键矩形的大小写进日志；
// 只在设置了 KURA_UI_SHOTS=<输出目录> 时运行，CI 不受影响（同 TestRenderViewModeShots）。
// 整窗用 8 个 mtime 各不相同的技能，三种排序各一张；筛选栏单独渲染最窄的几个宽度。
func TestRenderSortShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过排序开关截图")
	}
	a, cfg := newTestApp(t)
	for name, hours := range map[string]int{
		"api-design": 12, "brainstorm": 90, "code-review": 55, "debug-trace": 3,
		"pdf-tools": 70, "release-notes": 31, "tdd": 8, "web-search": 120,
	} {
		seedTimed(t, cfg, name, hours)
	}
	shoot := func(name string, w, h int, pref store.SortPref) {
		a.SetSortAsync(pref)
		for _, dark := range []bool{true, false} {
			tt := newTester(a, w, h)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			a.settle()
			tt.Frame()
			a.settle()
			tt.Frame()
			suffix := "light"
			if dark {
				suffix = "dark"
			}
			saveShot(t, out, name+"-"+suffix, tt)
		}
	}
	shoot("sort-newest", 1280, 800, prefNewest)
	shoot("sort-oldest", 1280, 800, prefOldest)
	shoot("sort-name", 1280, 800, prefName)
	a.selName = "tdd"
	shoot("sort-newest-detail-narrow", 900, 640, prefNewest)

	// 筛选栏单独渲染：用户把两侧栏拖到最宽时的最窄中栏（300）、默认窗口最小宽度且详情栏打开时的中栏
	// （334）、常见的 360 与 560。
	a.sortPref = prefNewest
	for _, width := range []int{300, 334, 360, 560} {
		for _, dark := range []bool{true, false} {
			tt := myui.NewTester(func(c *myui.Context) {
				mode := store.ThemeLight
				if dark {
					mode = store.ThemeDark
				}
				a.skin = kuraScheme.apply(c, mode)
				a.pal = a.skin.pal
				a.viewFilterBar(c)
			}, width, 130)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			tt.Frame()
			suffix := "light"
			if dark {
				suffix = "dark"
			}
			saveShot(t, out, "filterbar-"+suffix+"-w"+strconv.Itoa(width), tt)
			if !dark {
				for _, l := range []string{locales.UiSearchPlaceholder(), locales.UiSortName(), locales.UiSortTimeNewest(), locales.UiViewGrid(), locales.UiViewList()} {
					r, _ := tt.Find(l)
					t.Logf("宽 %d：%-12s X=%6.1f Y=%5.1f W=%6.1f H=%5.1f", width, l, r.X, r.Y, r.W, r.H)
				}
			}
		}
	}
}
