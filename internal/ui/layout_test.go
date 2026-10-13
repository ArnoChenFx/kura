package ui

// 三栏布局的界面测试：卡片网格、状态筛选、搜索、选择与详情。
// 全部数据造在 t.TempDir() 里（见 newTestApp）。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// showApp 启动 Tester 驱动 a.View，并走完首帧触发的初始扫描。
func showApp(a *App, w, h int) *myui.Tester {
	tt := newTester(a, w, h)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()
	return tt
}

// refresh 让界面完成一次「输入 → 后台任务 → update 回放 → 重绘」。
func refresh(a *App, tt *myui.Tester) {
	a.settle()
	tt.Frame()
}

// click 点击 s 并让界面跟上。
func click(t *testing.T, a *App, tt *myui.Tester, s string) {
	t.Helper()
	if err := tt.Click(s); err != nil {
		t.Fatal(err)
	}
	refresh(a, tt)
}

// countText 统计上一帧里恰好等于 s 的文本个数。
func countText(tt *myui.Tester, s string) int {
	n := 0
	for _, got := range tt.Texts() {
		if got == s {
			n++
		}
	}
	return n
}

// TestCardsFlowInRows 验证卡片按窗口宽度一行排多张，窄了再换行成一列。
func TestCardsFlowInRows(t *testing.T) {
	a, cfg := newTestApp(t)
	names := []string{"s1", "s2", "s3", "s4", "s5", "s6"}
	for _, n := range names {
		seedSkill(t, cfg, n, nil)
	}

	tt := showApp(a, 1280, 800)
	first, ok := tt.Find("s1")
	if !ok {
		t.Fatalf("应显示 s1，文本 %v", tt.Texts())
	}
	inRow := 0
	var lastX float32 = -1
	for _, n := range names {
		r, ok := tt.Find(n)
		if !ok {
			t.Fatalf("应显示 %s，文本 %v", n, tt.Texts())
		}
		if r.Y == first.Y {
			inRow++
			if r.X <= lastX {
				t.Errorf("同一行的卡片应从左到右排列：%s 在 x=%v，前一张在 x=%v", n, r.X, lastX)
			}
			lastX = r.X
		}
	}
	if inRow < 3 {
		t.Errorf("1280 宽的窗口一行应至少排 3 张卡片，得到 %d 张", inRow)
	}

	// 窄到放不下两列：每行一张。
	tt.SetSize(640, 800)
	refresh(a, tt)
	r1, _ := tt.Find("s1")
	r2, ok := tt.Find("s2")
	if !ok {
		t.Fatalf("窄窗口里应能找到 s2（可能在滚动区外），文本 %v", tt.Texts())
	}
	if r2.Y <= r1.Y {
		t.Errorf("640 宽时 s2 应换到 s1 下面一行：s1.Y=%v s2.Y=%v", r1.Y, r2.Y)
	}
	if r2.X != r1.X {
		t.Errorf("单列时 s1、s2 应左对齐：s1.X=%v s2.X=%v", r1.X, r2.X)
	}
}

// TestCardShowsDescriptionAndSelects 验证卡片上有描述；点击卡片后右栏出现，
// 切到另一张卡片时右栏跟着换。
func TestCardShowsDescriptionAndSelects(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能的描述", nil)
	seedDescribed(t, cfg, "beta", "第二个技能的描述", nil)
	tt := showApp(a, 1280, 800)

	for _, want := range []string{"第一个技能的描述", "第二个技能的描述"} {
		if countText(tt, want) != 1 {
			t.Errorf("卡片上应有一份 %q，文本 %v", want, tt.Texts())
		}
	}
	if _, ok := tt.Find("关闭详情"); ok {
		t.Error("没选中卡片时不应有右栏")
	}

	click(t, a, tt, "alpha")
	if a.selName != "alpha" {
		t.Fatalf("selName = %q，想要 alpha", a.selName)
	}
	// 描述现在卡片和右栏各一份。
	if got := countText(tt, "第一个技能的描述"); got != 2 {
		t.Errorf("选中后 alpha 的描述应出现在卡片和右栏，共 2 份，得到 %d；文本 %v", got, tt.Texts())
	}
	for _, want := range []string{"关闭详情", "详情:检查更新", "详情:移入回收站", "停用"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("右栏应有 %q，文本 %v", want, tt.Texts())
		}
	}

	// 另一张卡片：右栏换成 beta 的描述。
	click(t, a, tt, "beta")
	if a.selName != "beta" {
		t.Fatalf("selName = %q，想要 beta", a.selName)
	}
	if got := countText(tt, "第二个技能的描述"); got != 2 {
		t.Errorf("选中 beta 后其描述应出现 2 份，得到 %d；文本 %v", got, tt.Texts())
	}
	if got := countText(tt, "第一个技能的描述"); got != 1 {
		t.Errorf("alpha 的描述应只剩卡片上的一份，得到 %d", got)
	}
}

// TestFilterTabs 验证四个筛选标签：全部不含回收站，其余各看各的状态。
func TestFilterTabs(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "on1", nil)
	seedSkill(t, cfg, "on2", nil)
	seedSkill(t, cfg, "off1", nil)
	seedSkill(t, cfg, "bin1", nil)
	if _, err := cfg.Disable("off1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Remove("bin1"); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	// shown 返回当前显示哪些卡片（回收站卡片没有开关，用名字判断）。
	shown := func() map[string]bool {
		got := map[string]bool{}
		for _, n := range []string{"on1", "on2", "off1", "bin1"} {
			got[n] = tt.HasText(n+" 开关") || (n == "bin1" && tt.HasText("bin1"))
		}
		return got
	}
	cases := []struct {
		tab  string
		want map[string]bool
	}{
		{"启用 2", map[string]bool{"on1": true, "on2": true}},
		{"停用 1", map[string]bool{"off1": true}},
		{"回收站 1", map[string]bool{"bin1": true}},
		{"全部 3", map[string]bool{"on1": true, "on2": true, "off1": true}},
	}
	for _, c := range cases {
		click(t, a, tt, c.tab)
		got := shown()
		for _, n := range []string{"on1", "on2", "off1", "bin1"} {
			if got[n] != c.want[n] {
				t.Errorf("筛选 %q：%s 显示=%v，想要 %v；文本 %v", c.tab, n, got[n], c.want[n], tt.Texts())
			}
		}
	}
}

// TestSearchMatchesDescription 验证搜索同时匹配名字与描述，无结果时给出
// 针对搜索词的空态。
func TestSearchMatchesDescription(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "处理 PDF 文件", nil)
	seedDescribed(t, cfg, "beta", "发送邮件", nil)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "搜索技能")
	tt.Type("pdf")
	refresh(a, tt)
	if !tt.HasText("alpha 开关") || tt.HasText("beta 开关") {
		t.Errorf("按描述 pdf 搜索应只剩 alpha，文本 %v", tt.Texts())
	}

	tt.Key(myui.Ctrl, myui.KeyA)
	tt.Type("zzz")
	refresh(a, tt)
	if !tt.HasText("没有匹配「zzz」的技能") {
		t.Errorf("无结果时应提示没有匹配，文本 %v", tt.Texts())
	}
}

// TestEmptyGridMessages 验证空网格按情境给出不同的提示。
func TestEmptyGridMessages(t *testing.T) {
	a, cfg := newTestApp(t)
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	if !tt.HasText("这个目录里还没有技能") || !tt.HasText("安装技能") {
		t.Errorf("空目录应引导去安装，文本 %v", tt.Texts())
	}

	click(t, a, tt, "停用 0")
	if !tt.HasText("没有已停用的技能") {
		t.Errorf("停用筛选下为空应说明，文本 %v", tt.Texts())
	}
}

// TestSelectionSurvivesFilter 验证：选中的技能被筛选藏起来时，右栏仍然
// 停在它身上；切回去后卡片重新被高亮。
func TestSelectionSurvivesFilter(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedSkill(t, cfg, "off", nil)
	if _, err := cfg.Disable("off"); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "alpha")
	click(t, a, tt, "停用 1")
	if a.selName != "alpha" || a.selIdx != -1 {
		t.Errorf("alpha 被筛掉后应保留名字、网格里无选中：selName=%q selIdx=%d", a.selName, a.selIdx)
	}
	if tt.HasText("alpha 开关") {
		t.Error("alpha 的卡片应被筛掉")
	}
	if _, ok := tt.Find("关闭详情"); !ok {
		t.Errorf("右栏应仍停在 alpha 上，文本 %v", tt.Texts())
	}

	click(t, a, tt, "全部 2")
	if a.selName != "alpha" || a.selIdx != 0 {
		t.Errorf("切回全部后 alpha 应重新被选中：selName=%q selIdx=%d", a.selName, a.selIdx)
	}
}

// TestCardTags 验证卡片上的状态标签：有更新、已停用、元数据损坏、缺少 SKILL.md。
func TestCardTags(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "fresh", nil)
	seedSkill(t, cfg, "stale", nil)
	seedSkill(t, cfg, "off", nil)
	if _, err := cfg.Disable("off"); err != nil {
		t.Fatal(err)
	}
	bad := cfg.SkillPath("bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, store.MetaFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	hollow := cfg.SkillPath("hollow")
	if err := os.MkdirAll(hollow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hollow, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.updates["stale"] = true

	tt := showApp(a, 1400, 900)
	for _, want := range []string{"有更新", "已停用", "元数据损坏", "缺少 SKILL.md"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("卡片应带标签 %q，文本 %v", want, tt.Texts())
		}
	}
	// 只有 stale 有更新。
	if got := countText(tt, "有更新"); got != 1 {
		t.Errorf("「有更新」应只有 1 个，得到 %d", got)
	}
}

// TestMinimumWindowStaysUsable 验证最小窗口（含右栏）下主要控件都在。
func TestMinimumWindowStaysUsable(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "一个很长很长的描述，用来确认卡片在窄窗口里不会把布局撑坏。", nil)
	seedSkill(t, cfg, "beta", nil)
	tt := showApp(a, 880, 520)
	click(t, a, tt, "alpha")

	for _, want := range []string{"搜索技能", "安装技能", "添加文件夹", "刷新", "关闭详情", "alpha 开关"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("最小窗口里应找得到 %q，文本 %v", want, tt.Texts())
		}
	}
}

// TestStaleResultsAreDropped 验证切换目录后，旧目录还在路上的结果不会覆盖
// 新目录的列表。
func TestStaleResultsAreDropped(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "from-default", nil)
	other := makeRoot(t, "extra", "from-extra")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	showApp(a, 980, 640)

	// 默认目录的扫描已发出，结果还没回放时用户切到了另一个目录。
	a.ReloadAsync()
	a.SelectRoot(other)
	a.flush()
	if got := skillNames(a.skills); len(got) != 1 || got[0] != "from-extra" {
		t.Errorf("切换后列表应只有新目录的技能，得到 %v", got)
	}

	// 默认目录里的操作在切换后才完成：忙碌标记清掉，列表不被旧结果覆盖。
	// 同步的 spawn 让两个结果按入队顺序回放，这里把顺序对调，模拟旧操作
	// 比新目录的扫描更晚完成。
	a.SelectRoot(a.lib.Default())
	a.flush()
	a.ToggleAsync("from-default", false)
	a.SelectRoot(other)
	if len(a.pending) != 2 {
		t.Fatalf("应有操作结果与新目录扫描两个待回放的结果，得到 %d 个", len(a.pending))
	}
	a.pending[0], a.pending[1] = a.pending[1], a.pending[0]
	a.flush()
	if len(a.busy) != 0 {
		t.Errorf("操作完成后忙碌标记应清空，得到 %v", a.busy)
	}
	if got := skillNames(a.skills); len(got) != 1 || got[0] != "from-extra" {
		t.Errorf("旧目录的操作结果不应覆盖当前列表，得到 %v", got)
	}
	if _, err := os.Stat(a.lib.Config(a.lib.Default()).DisabledPath("from-default")); err != nil {
		t.Errorf("操作本身应已在默认目录完成: %v", err)
	}
}

// skillNames 取技能名列表。
func skillNames(skills []*store.Skill) []string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

// makeRoot 在临时目录里造一个额外的技能目录，里面有若干最小技能。
func makeRoot(t *testing.T, name string, skills ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		sd := filepath.Join(dir, s)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + s + "\n---\n\n# " + s + "\n"
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
