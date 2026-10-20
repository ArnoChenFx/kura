package ui

// 第一轮界面反馈的测试：分割线拖动改宽、左栏目录拖动排序、回收站永久删除、名字排序
// （停用的技能不沉底）、全文译文、启动时自动发现 agent 目录、标题栏图标。
// 数据全在 t.TempDir() 里；翻译一律用假实现。

import (
	"github.com/ArnoChenFx/kura/locales"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// dragH 在 (x, y) 按下分割线，水平拖动 dx 后松开。每一步都让界面完整刷新一帧，
// 分割线才能逐帧收到拖动量。
func dragH(a *App, tt *myui.Tester, x, y, dx float32) {
	tt.Press(x, y)
	tt.Frame()
	const steps = 4
	for i := float32(1); i <= steps; i++ {
		tt.Move(x+dx*i/steps, y)
		tt.Frame()
	}
	tt.Release(x+dx, y)
	refresh(a, tt)
}

// pathExists 报告 p 是否存在（文件或目录）。
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestSplittersDragColumnWidths 验证两条分割线：左栏向右拖变宽、详情栏向左拖变宽；
// 拖过头就停在上下限，中栏不会被挤没。
func TestSplittersDragColumnWidths(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能", nil)
	tt := showApp(a, 1280, 800)
	const y = 400

	dragH(a, tt, a.rootsW+dividerWidth/2.0, y, 60)
	if a.rootsW != rootsWidth+60 {
		t.Errorf("左分割线向右拖 60 后左栏宽 = %v，想要 %v", a.rootsW, rootsWidth+60)
	}
	dragH(a, tt, a.rootsW+dividerWidth/2.0, y, 1000)
	if a.rootsW != rootsMaxW {
		t.Errorf("左栏拖过头应停在上限 %v，得到 %v", rootsMaxW, a.rootsW)
	}
	dragH(a, tt, a.rootsW+dividerWidth/2.0, y, -1000)
	if a.rootsW != rootsMinW {
		t.Errorf("左栏拖过头应停在下限 %v，得到 %v", rootsMinW, a.rootsW)
	}

	// 选中技能后出现详情栏，它的分割线在右侧（向左拖变宽）。
	click(t, a, tt, "alpha")
	dragH(a, tt, float32(1280)-a.detailW-dividerWidth/2.0, y, -100)
	if a.detailW != detailWidth+100 {
		t.Errorf("详情栏向左拖 100 后宽 = %v，想要 %v", a.detailW, detailWidth+100)
	}
	dragH(a, tt, float32(1280)-a.detailW-dividerWidth/2.0, y, 1000)
	if a.detailW != detailMinW {
		t.Errorf("详情栏拖过头应停在下限 %v，得到 %v", detailMinW, a.detailW)
	}
}

// TestRootListDragReordersExtraRoots 验证左栏追加的目录可以拖动排序：把第二个目录
// 拖到第一个目录上，列表顺序随之改变并写进 config.json；默认目录始终在第一个，
// 也不能被挪走。
func TestRootListDragReordersExtraRoots(t *testing.T) {
	base := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(base, "skills"), filepath.Join(base, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	extraA := filepath.Join(base, "extra-a")
	extraB := filepath.Join(base, "extra-b")
	for _, d := range []string{extraA, extraB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{extraA, extraB} {
		if err := lib.AddRoot(d); err != nil {
			t.Fatal(err)
		}
	}
	a := newAppFor(cfg)
	tt := showApp(a, 1280, 800)
	before := []string{cfg.Root, extraA, extraB}
	if !slices.Equal(a.lib.Roots, before) {
		t.Fatalf("初始目录顺序 = %v，想要 %v", a.lib.Roots, before)
	}

	// 从 extra-b 的标签按下，逐步拖到 extra-a 的标签上再松开。
	src, ok1 := tt.Find(rootLabel(extraB))
	dst, ok2 := tt.Find(rootLabel(extraA))
	if !ok1 || !ok2 {
		t.Fatalf("左栏里找不到目录标签，文本 %v", tt.Texts())
	}
	sx, sy := src.X+src.W/2, src.Y+src.H/2
	dx, dy := dst.X+dst.W/2, dst.Y+dst.H/2
	tt.Press(sx, sy)
	tt.Frame()
	for i := float32(1); i <= 4; i++ {
		tt.Move(sx+(dx-sx)*i/4, sy+(dy-sy)*i/4)
		tt.Frame()
	}
	tt.Release(dx, dy)
	refresh(a, tt)
	refresh(a, tt)

	want := []string{cfg.Root, extraB, extraA}
	if !slices.Equal(a.lib.Roots, want) {
		t.Errorf("拖动后目录顺序 = %v，想要 %v", a.lib.Roots, want)
	}
	saved, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(saved.Roots, want) {
		t.Errorf("新顺序应写进 config.json，重新读取得到 %v", saved.Roots)
	}

	// 默认目录不能被挪走：挪到第二位会报错，顺序不变。
	a.ReorderRootAsync(cfg.Root, 1)
	a.settle()
	if !a.statusErr || !slices.Equal(a.lib.Roots, want) {
		t.Errorf("默认目录不应能挪走：statusErr=%v roots=%v", a.statusErr, a.lib.Roots)
	}
}

// TestPurgeFromTrashConfirmsAndKeepsSnapshot 验证回收站里的技能「永久删除…」先过确认框；
// 确认后它从回收站消失，删除前写下的快照还留在 kura 的数据目录里。
func TestPurgeFromTrashConfirmsAndKeepsSnapshot(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能", nil)
	tt := showApp(a, 1280, 800)

	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "移入回收站…")
	click(t, a, tt, "确认移入")
	trash := cfg.TrashPath("alpha")
	if !pathExists(trash) {
		t.Fatal("移入回收站后应有回收站副本")
	}

	click(t, a, tt, "回收站 1")
	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "永久删除…")
	if !pathExists(trash) || !tt.HasText("永久删除？") {
		t.Fatal("选「永久删除…」应先弹出确认框，且此时还不能删")
	}
	// 详情栏也有一个「永久删除」按钮，按标签点会点到它；确认框的主按钮自动获得焦点，
	// 回车即确认，这也是真实的键盘路径。
	tt.Key(0, myui.KeyEnter)
	refresh(a, tt)

	if pathExists(trash) {
		t.Error("确认后回收站里不应再有 alpha")
	}
	snaps, err := os.ReadDir(filepath.Join(cfg.Home, "snapshots", "alpha"))
	if err != nil || len(snaps) == 0 {
		t.Fatalf("删除前应留有快照，读取 %v err=%v", snaps, err)
	}
	if !tt.HasText("回收站 0") {
		t.Errorf("回收站计数应归零，文本 %v", tt.Texts())
	}
}

// TestSkillListSortsByNameKeepsDisabledInPlace 验证技能列表默认按名字排序（不区分大小写），
// 停用的技能留在名字该在的位置，不沉到最后。
func TestSkillListSortsByNameKeepsDisabledInPlace(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "charlie", "第三个", nil)
	seedDescribed(t, cfg, "Alpha", "第一个", nil)
	seedDescribed(t, cfg, "bravo", "第二个", nil)
	if _, err := cfg.Disable("bravo"); err != nil {
		t.Fatal(err)
	}
	showApp(a, 1280, 800)
	var got []string
	for _, s := range a.visible() {
		got = append(got, s.Name)
	}
	if want := []string{"Alpha", "bravo", "charlie"}; !slices.Equal(got, want) {
		t.Errorf("列表顺序 = %v，想要 %v（停用的 bravo 不应沉到最后）", got, want)
	}
}

// TestFullTextShowsTranslationAndSendsWholeDoc 验证译文模式下全文页显示译文，并注明
// 「仅界面显示」；送去翻译的是 SKILL.md 的全文，文件本身一字不动。
func TestFullTextShowsTranslationAndSendsWholeDoc(t *testing.T) {
	a, cfg, f, tt := trApp(t, true)
	body := "---\nname: alpha\ndescription: Reads PDF files.\n---\n\n# alpha\n\n这是原文段落，译文要显示。\n"
	path := filepath.Join(cfg.SkillPath("alpha"), "SKILL.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.settle()
	tt.Frame()

	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	calls := f.calls
	detClickTab(t, a, tt, locales.UiDetailTabFull())

	if !detHasText(tt, "仅界面显示，不改动技能文件") {
		t.Errorf("译文全文页应注明仅界面显示，文本 %v", tt.Texts())
	}
	if !detHasText(tt, "[译]---") {
		t.Errorf("全文译文应显示出来，文本 %v", tt.Texts())
	}
	if f.calls != calls+1 || !strings.Contains(f.texts[len(f.texts)-1], "这是原文段落，译文要显示。") {
		t.Errorf("全文应整段送去翻译（只多一次请求），calls=%d→%d texts=%q", calls, f.calls, f.texts)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Error("全文翻译不应改动 SKILL.md")
	}
}

// TestDiscoverAgentRootsAsyncAddsAndKeepsDefaultFirst 验证启动时自动发现：本机已有的
// agent 技能目录加入列表，默认目录仍在第一个，状态行说明加入了几个。
func TestDiscoverAgentRootsAsyncAddsAndKeepsDefaultFirst(t *testing.T) {
	a, cfg := newTestApp(t)
	home := t.TempDir()
	claude := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	a.DiscoverAgentRootsAsync(home)
	refresh(a, tt)
	if want := []string{cfg.Root, claude}; !slices.Equal(a.lib.Roots, want) {
		t.Errorf("发现后目录 = %v，想要 %v", a.lib.Roots, want)
	}
	if want := "已自动加入 1 个 agent 技能目录"; a.status != want || a.statusErr {
		t.Errorf("状态行 = %q（错误=%v），想要 %q", a.status, a.statusErr, want)
	}
	if _, ok := a.rootCounts[claude]; !ok {
		t.Error("新加入的目录应统计在用技能数")
	}
}

// TestTitlebarShowsAppIconNotLetter 验证标题栏用项目里的图标文件做标识：左侧画出了
// 图标，不再有单独的字母「K」，名字是「kura」。
func TestTitlebarShowsAppIconNotLetter(t *testing.T) {
	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)
	if tt.HasText("K") {
		t.Error("标题栏不应再用文字 K 做标识")
	}
	if !tt.HasText("kura") {
		t.Errorf("标题栏应显示名字 kura，文本 %v", tt.Texts())
	}
	img := tt.Image()
	diff := 0
	for y := 0; y < titleBarMinHeight; y++ {
		for x := 8; x < 40; x++ {
			if !near(img.RGBAAt(x, y), a.pal.sidebar, 12) {
				diff++
			}
		}
	}
	if diff < 100 {
		t.Errorf("标题栏左侧应画出图标，发现的非底色像素只有 %d 个", diff)
	}
}

// TestTrashedMenuHasPermanentDeleteInBothViews 验证回收站里的技能，网格与列表两种排列下，
// 右键名字与右键「恢复」按钮弹出的菜单都有「永久删除…」。
func TestTrashedMenuHasPermanentDeleteInBothViews(t *testing.T) {
	want := []string{"打开文件夹", "在文件夹中显示", "复制路径", "-", "恢复到技能目录", "-", "永久删除…"}
	for _, list := range []bool{false, true} {
		a, cfg := newTestApp(t)
		seedDescribed(t, cfg, "keep", "活着的", nil)
		seedDescribed(t, cfg, "gone", "回收站里的", nil)
		if _, err := cfg.Remove("gone"); err != nil {
			t.Fatal(err)
		}
		tt := showApp(a, 1280, 800)
		a.filter = filterTrashed
		if list {
			a.viewMode = store.ViewList
		}
		refresh(a, tt)
		if got := openMenu(t, a, tt, "gone"); !slices.Equal(got, want) {
			t.Errorf("列表=%v 时右键名字的菜单 = %q，想要 %q", list, got, want)
		}
		tt.CloseMenu()
		if err := tt.RightClick("恢复"); err != nil {
			t.Fatalf("列表=%v 时点不到「恢复」按钮: %v", list, err)
		}
		refresh(a, tt)
		if got := tt.Menu(); !slices.Equal(got, want) {
			t.Errorf("列表=%v 时右键「恢复」按钮的菜单 = %q，想要 %q", list, got, want)
		}
		tt.CloseMenu()
	}
}
