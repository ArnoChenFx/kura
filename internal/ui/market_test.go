package ui

// 技能市场页面的测试。榜单与快照用假读取函数（App.marketFetch / marketDocFetch）回放
// 预设结果；安装用 t.TempDir() 里的本地仓库（经 insteadOf 冒充 GitHub）；外部链接用记录
// 用的假实现。全部不联网，也不碰真实的技能目录与浏览器。

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// marketResp 是假读取函数对榜单某一页的回应。
type marketResp struct {
	page market.Page
	err  error
}

// docResp 是假读取函数对某个技能快照的回应。
type docResp struct {
	doc market.Document
	err error
}

// fakeMarket 替换 App.marketFetch 与 App.marketDocFetch：按「榜单/页码」与「来源/名字」回放
// 预设结果，没有预设的组合报错。calls 与 docCalls 记录每一次读取的次序。
type fakeMarket struct {
	mu       sync.Mutex
	resp     map[string]marketResp
	calls    []string
	docs     map[string]docResp
	docCalls []string
}

func newFakeMarket() *fakeMarket {
	return &fakeMarket{resp: map[string]marketResp{}, docs: map[string]docResp{}}
}

// set 预设榜单 v 第 page 页的回应（可在两次读取之间改写）。
func (f *fakeMarket) set(v market.View, page int, r marketResp) {
	f.resp[marketKeyOf(v, page)] = r
}

// setDoc 预设 source 仓库里名为 name 的技能快照。
func (f *fakeMarket) setDoc(source, name string, r docResp) {
	f.docs[source+"/"+name] = r
}

func (f *fakeMarket) fetch(_ context.Context, v market.View, page int) (market.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := marketKeyOf(v, page)
	f.calls = append(f.calls, key)
	if r, ok := f.resp[key]; ok {
		return r.page, r.err
	}
	return market.Page{}, errors.New("no canned response for " + key)
}

func (f *fakeMarket) fetchDoc(_ context.Context, source, name string) (market.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := source + "/" + name
	f.docCalls = append(f.docCalls, key)
	if r, ok := f.docs[key]; ok {
		return r.doc, r.err
	}
	return market.Document{}, errors.New("no canned document for " + key)
}

// marketKeyOf 是测试里「榜单/页码」的写法，与 fakeMarket.calls 的格式一致。
func marketKeyOf(v market.View, page int) string { return string(v) + "/" + strconv.Itoa(page) }

// urlRecorder 替换 App.openURL：记录打开过的链接，可指定失败。
type urlRecorder struct {
	mu   sync.Mutex
	urls []string
	err  error
}

func (r *urlRecorder) open(u string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = append(r.urls, u)
	return r.err
}

func (r *urlRecorder) opened() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...)
}

// okPage 构造一页成功的回应。
func okPage(hasMore bool, skills ...market.Skill) marketResp {
	return marketResp{page: market.Page{Skills: skills, HasMore: hasMore}}
}

// mkSkill 造一个榜单条目，来源是 owner/<name>。
func mkSkill(name string, installs int64) market.Skill {
	return market.Skill{Source: "owner/" + name, SkillID: name, Name: name, Installs: installs}
}

// seedMarketRepo 在 t.TempDir() 里造一个仓库（含给定的技能目录），并让 alias 指向它。
func seedMarketRepo(t *testing.T, alias string, dirs ...string) {
	t.Helper()
	repo := seedRepo(t, t.TempDir(), "", dirs...)
	mirrorAlias(t, repo, alias)
}

// installMarketApp 建一个应用，并替换好假的榜单、快照与链接函数。
func installMarketApp(t *testing.T) (*App, *fakeMarket, *urlRecorder) {
	t.Helper()
	a, _ := newTestApp(t)
	f := newFakeMarket()
	a.marketFetch = f.fetch
	a.marketDocFetch = f.fetchDoc
	u := &urlRecorder{}
	a.openURL = u.open
	return a, f, u
}

// TestMarketOpenLoadsAllTimeFirstPage 验证打开页面读总榜第一页；内容记在总榜名下，
// 没切到过的榜单不会被创建（渲染路径不改应用状态）。
func TestMarketOpenLoadsAllTimeFirstPage(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(true, mkSkill("tdd", 9)))

	a.OpenMarket()
	a.flush()

	if a.market.screen != marketList || strings.Join(f.calls, " ") != "all-time/0" {
		t.Fatalf("打开后应进入列表并只读总榜第一页：screen=%v calls=%v", a.market.screen, f.calls)
	}
	b := a.market.boardOr(market.ViewAllTime)
	if !b.loaded || len(b.skills) != 1 || !b.hasMore || b.next != 1 || b.err != "" {
		t.Errorf("总榜内容不对：%+v", b)
	}
	if _, ok := a.market.boards[market.ViewHot]; ok {
		t.Error("没有切到的榜单不应被创建")
	}
}

// TestMarketSwitchingBoardsReadsEachOnce 验证切换排序方式时读对应的榜单，切回来用缓存。
func TestMarketSwitchingBoardsReadsEachOnce(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 9)))
	f.set(market.ViewHot, 0, okPage(false, mkSkill("pdf", 3)))

	a.OpenMarket()
	a.flush()
	a.market.view = 1 // 分段点到「热度」
	a.ensureMarketBoard()
	a.flush()
	a.market.view = 0 // 切回总榜：内容已在，不重读
	a.ensureMarketBoard()
	a.flush()

	if strings.Join(f.calls, " ") != "all-time/0 hot/0" {
		t.Errorf("读取次序 = %v，想要 [all-time/0 hot/0]", f.calls)
	}
	if got := a.market.boardOr(market.ViewHot).skills; len(got) != 1 || got[0].Name != "pdf" {
		t.Errorf("热度榜内容不对：%+v", got)
	}
}

// TestMarketLoadMoreAppendsNextPage 验证「加载更多」接着读下一页；最后一页之后不再有。
func TestMarketLoadMoreAppendsNextPage(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(true, mkSkill("a", 3), mkSkill("b", 2)))
	f.set(market.ViewAllTime, 1, okPage(false, mkSkill("c", 1)))

	a.OpenMarket()
	a.flush()
	a.loadMarketPageAsync(market.ViewAllTime, a.market.boardOr(market.ViewAllTime).next)
	a.flush()

	b := a.market.boardOr(market.ViewAllTime)
	if len(b.skills) != 3 || b.skills[2].Name != "c" || b.hasMore || b.next != 2 {
		t.Errorf("追加后的榜单不对：%+v", b)
	}
}

// TestMarketFailureKeepsReasonAndRetries 验证读取失败时保留原因，重试读的是同一页。
func TestMarketFailureKeepsReasonAndRetries(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, marketResp{err: errors.New("network down")})

	a.OpenMarket()
	a.flush()
	b := a.market.boardOr(market.ViewAllTime)
	if b.loaded || b.err == "" || b.loading {
		t.Fatalf("失败后应记下原因且不算已加载：%+v", b)
	}

	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 9)))
	a.loadMarketPageAsync(market.ViewAllTime, b.next)
	a.flush()
	if b = a.market.boardOr(market.ViewAllTime); !b.loaded || b.err != "" || len(b.skills) != 1 {
		t.Errorf("重试后应读到内容且清掉原因：%+v", b)
	}
}

// TestMarketInstallsIntoCurrentRoot 验证从页面装一个技能：装进当前目录、选中新技能，装上后
// 判定为已安装，同仓库的其他技能不被带进来。
func TestMarketInstallsIntoCurrentRoot(t *testing.T) {
	a, _, _ := installMarketApp(t)
	cfg := a.cfg
	seedMarketRepo(t, "owner/mkt", "skills/tdd", "skills/other")

	a.InstallMarketSkillAsync(market.Skill{Source: "owner/mkt", Name: "tdd"})
	a.flush()

	if a.market.installing != "" || a.market.err != "" || a.statusErr {
		t.Fatalf("安装应成功：installing=%q err=%q statusErr=%v", a.market.installing, a.market.err, a.statusErr)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("tdd"), "SKILL.md")); err != nil {
		t.Fatalf("技能应装进当前目录: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("other")); !os.IsNotExist(err) {
		t.Error("同仓库的其他技能不应被装上")
	}
	if a.selName != "tdd" {
		t.Errorf("新装的技能应被选中，selName=%q", a.selName)
	}
	if !a.installedNames()["tdd"] {
		t.Error("装上后应判定为已安装")
	}
}

// TestMarketInstallFailureReportsOnPage 验证装不上（仓库里没有这个技能）时页面保持打开、
// 原因留在页面里，什么都不装。
func TestMarketInstallFailureReportsOnPage(t *testing.T) {
	a, _, _ := installMarketApp(t)
	a.market.screen = marketList
	seedMarketRepo(t, "owner/mkt", "skills/tdd")

	a.InstallMarketSkillAsync(market.Skill{Source: "owner/mkt", Name: "missing"})
	a.flush()

	if a.market.screen != marketList || a.market.err == "" || !a.statusErr || a.market.installing != "" {
		t.Errorf("失败应留在页面里报告：screen=%v err=%q statusErr=%v", a.market.screen, a.market.err, a.statusErr)
	}
	if _, err := os.Stat(a.cfg.SkillPath("missing")); !os.IsNotExist(err) {
		t.Error("失败时不应留下技能目录")
	}
}

// TestMarketInstallStaleRootDropsSelection 验证安装途中切换了目录：旧目录的结果不改选中，
// 也不覆盖当前列表（同 startInstall 的 gen 守卫）。
func TestMarketInstallStaleRootDropsSelection(t *testing.T) {
	a, _, _ := installMarketApp(t)
	seedMarketRepo(t, "owner/stale", "skills/tdd")
	var pending []func()
	a.spawn = func(fn func()) { pending = append(pending, fn) }

	a.InstallMarketSkillAsync(market.Skill{Source: "owner/stale", Name: "tdd"})
	a.gen++ // 等同于途中切换了目录（SelectRoot 会递增 gen）
	for _, fn := range pending {
		fn()
	}
	a.flush()

	if a.selName == "tdd" {
		t.Error("切换目录后，旧目录的安装结果不应改选中")
	}
	if a.market.installing != "" {
		t.Error("结束后不应残留「正在安装」")
	}
}

// TestMarketDetailLoadsDocumentOnce 验证详情页读快照：第一次打开读一次，之后用缓存；
// 描述、文件与 SKILL.md 原文都记下来。
func TestMarketDetailLoadsDocumentOnce(t *testing.T) {
	a, f, _ := installMarketApp(t)
	sk := mkSkill("tdd", 9)
	f.setDoc(sk.Source, sk.Name, docResp{doc: market.Document{
		Description: "Write tests first", SkillMD: "# TDD", Files: []string{"SKILL.md", "examples/a.md"}, Hash: "h",
	}})

	a.OpenMarketDetail(sk)
	a.flush()
	a.CloseMarketDetail()
	a.OpenMarketDetail(sk)
	a.flush()

	if a.market.screen != marketDetail || strings.Join(f.docCalls, " ") != "owner/tdd/tdd" {
		t.Fatalf("应只读一次快照：screen=%v docCalls=%v", a.market.screen, f.docCalls)
	}
	d := a.market.docOr(marketKey(sk))
	if !d.loaded || d.doc.Description != "Write tests first" || d.doc.SkillMD != "# TDD" || len(d.doc.Files) != 2 {
		t.Errorf("快照内容不对：%+v", d)
	}
}

// TestMarketDetailDocFailureRetries 验证快照读取失败时保留原因，重试后读到内容。
func TestMarketDetailDocFailureRetries(t *testing.T) {
	a, f, _ := installMarketApp(t)
	sk := mkSkill("pdf", 5)
	f.setDoc(sk.Source, sk.Name, docResp{err: errors.New("offline")})

	a.OpenMarketDetail(sk)
	a.flush()
	d := a.market.docOr(marketKey(sk))
	if d.loaded || d.err == "" || d.loading {
		t.Fatalf("失败后应记下原因：%+v", d)
	}

	f.setDoc(sk.Source, sk.Name, docResp{doc: market.Document{SkillMD: "# PDF"}})
	a.loadMarketDocAsync(sk)
	a.flush()
	if d = a.market.docOr(marketKey(sk)); !d.loaded || d.err != "" || d.doc.SkillMD != "# PDF" {
		t.Errorf("重试后应读到内容：%+v", d)
	}
}

// TestMarketOpenURLRunsInBackground 验证外部链接在后台打开；失败只在状态行报告。
func TestMarketOpenURLRunsInBackground(t *testing.T) {
	a, _, u := installMarketApp(t)
	var pending []func()
	a.spawn = func(fn func()) { pending = append(pending, fn) }

	a.openMarketURL("https://skills.sh/owner/tdd/tdd")
	if len(u.opened()) != 0 {
		t.Fatal("链接应在后台打开，不在发起的那一帧里同步调用")
	}
	for _, fn := range pending {
		fn()
	}
	a.flush()
	if got := u.opened(); len(got) != 1 || got[0] != "https://skills.sh/owner/tdd/tdd" || a.statusErr {
		t.Errorf("应打开链接且不报错：opened=%v statusErr=%v", got, a.statusErr)
	}

	u.err = errors.New("no browser")
	a.spawn = func(fn func()) { fn() }
	a.openMarketURL("https://github.com/owner/tdd")
	a.flush()
	if !a.statusErr || a.status == "" {
		t.Errorf("打开失败应在状态行报告：status=%q statusErr=%v", a.status, a.statusErr)
	}
}

// TestMarketPageRendersRowsAndInstalledMark 真实渲染：点标题栏的「技能市场」打开页面，
// 列表行显示名字、来源与安装次数；本目录里已有的技能显示「已安装」，其余行有「安装」按钮。
func TestMarketPageRendersRowsAndInstalledMark(t *testing.T) {
	a, f, _ := installMarketApp(t)
	seedSkill(t, a.cfg, "tdd", nil)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1234), mkSkill("pdf", 5)))
	tt := showApp(a, 1280, 800)

	click(t, a, tt, locales.UiActionMarket())
	if a.market.screen != marketList {
		t.Fatalf("点「技能市场」应打开页面，文本 %v", tt.Texts())
	}
	for _, want := range []string{"owner/tdd", "owner/pdf", locales.UiMarketInstalls(1234), locales.UiMarketInstalled()} {
		if !tt.HasText(want) {
			t.Errorf("页面应显示 %q，文本 %v", want, tt.Texts())
		}
	}
	if _, ok := tt.Find(locales.UiMarketInstall()); !ok {
		t.Errorf("没装过的 pdf 应有「安装」按钮，文本 %v", tt.Texts())
	}
}

// TestMarketRowOpensDetailWithMoreInfo 真实点击列表行的名字：进入详情页，显示描述、文件数，
// 渲染 SKILL.md 正文；点「返回榜单」回到列表。
func TestMarketRowOpensDetailWithMoreInfo(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1234)))
	f.setDoc("owner/tdd", "tdd", docResp{doc: market.Document{
		Description: "Write tests first",
		SkillMD:     "---\nname: tdd\n---\n\n# Test Driven Skill\n\nRed, green, refactor.\n",
		Files:       []string{"SKILL.md", "examples/login.md"},
	}})
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	click(t, a, tt, "tdd")
	if a.market.screen != marketDetail || a.market.detail.Name != "tdd" {
		t.Fatalf("点名字应进入 tdd 的详情页：screen=%v", a.market.screen)
	}
	for _, want := range []string{"Write tests first", locales.UiMarketFiles(2), "Test Driven Skill"} {
		if !tt.HasText(want) {
			t.Errorf("详情页应显示 %q，文本 %v", want, tt.Texts())
		}
	}

	click(t, a, tt, locales.UiMarketBackToBoard())
	if a.market.screen != marketList || !tt.HasText("owner/tdd") {
		t.Errorf("返回榜单后应回到列表，文本 %v", tt.Texts())
	}
}

// TestMarketInstallButtonDoesNotOpenDetail 验证行上的「安装」按钮只安装，不误开详情页。
func TestMarketInstallButtonDoesNotOpenDetail(t *testing.T) {
	a, f, _ := installMarketApp(t)
	seedMarketRepo(t, "owner/pdf", "skills/pdf")
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("pdf", 5)))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	click(t, a, tt, locales.UiMarketInstall())
	if a.market.screen != marketList {
		t.Errorf("点安装不应进入详情页，screen=%v", a.market.screen)
	}
	if !a.installedNames()["pdf"] || !tt.HasText(locales.UiMarketInstalled()) {
		t.Errorf("安装后应显示已安装，文本 %v", tt.Texts())
	}
}

// TestMarketBackReturnsToSkills 验证「返回技能目录」回到三栏的本地技能视图。
func TestMarketBackReturnsToSkills(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	click(t, a, tt, locales.UiMarketBackToSkills())
	if a.market.screen != marketNone {
		t.Fatalf("返回后应离开市场，screen=%v", a.market.screen)
	}
	if _, ok := tt.Find(rootLabel(a.cfg.Root)); !ok {
		t.Errorf("返回后应看到三栏的技能目录，文本 %v", tt.Texts())
	}
}

// TestMarketSegmentSwitchLoadsBoard 真实点击分段「热度」：读热度榜并显示它的内容。
func TestMarketSegmentSwitchLoadsBoard(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1)))
	f.set(market.ViewHot, 0, okPage(false, mkSkill("hotone", 99)))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	click(t, a, tt, locales.UiMarketViewHot())
	if strings.Join(f.calls, " ") != "all-time/0 hot/0" || !tt.HasText("owner/hotone") {
		t.Errorf("点「热度」应读热度榜并显示它：calls=%v 文本 %v", f.calls, tt.Texts())
	}
}

// TestMarketPageShowsFailureAndRetry 读取失败时页面显示原因与「重试」；网络恢复后点重试，
// 列表出现。
func TestMarketPageShowsFailureAndRetry(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, marketResp{err: errors.New("offline")})
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	if !tt.HasText(locales.UiMarketLoadFailed("offline")) || !tt.HasText(locales.UiMarketRetry()) {
		t.Fatalf("失败时应显示原因与重试，文本 %v", tt.Texts())
	}
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1)))
	click(t, a, tt, locales.UiMarketRetry())
	if !tt.HasText("owner/tdd") {
		t.Errorf("重试成功后应列出条目，文本 %v", tt.Texts())
	}
}

// TestMarketPageLoadMoreButton 有下一页时显示「加载更多」，点了就把下一页接在后面，读完
// 最后一页后按钮消失。
func TestMarketPageLoadMoreButton(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(true, mkSkill("first", 2)))
	f.set(market.ViewAllTime, 1, okPage(false, mkSkill("second", 1)))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())
	if !tt.HasText(locales.UiMarketLoadMore()) {
		t.Fatalf("还有下一页时应显示「加载更多」，文本 %v", tt.Texts())
	}

	click(t, a, tt, locales.UiMarketLoadMore())
	if !tt.HasText("owner/second") || tt.HasText(locales.UiMarketLoadMore()) {
		t.Errorf("读完下一页应接在后面并隐藏按钮，文本 %v", tt.Texts())
	}
}

// TestMarketDetailOpensSkillsShPage 详情页的「在 skills.sh 上查看」在后台打开该技能的页面。
func TestMarketDetailOpensSkillsShPage(t *testing.T) {
	a, f, u := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1)))
	f.setDoc("owner/tdd", "tdd", docResp{doc: market.Document{SkillMD: "# tdd"}})
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())
	click(t, a, tt, "tdd")

	click(t, a, tt, locales.UiMarketViewSkillsSh())
	if got := u.opened(); len(got) != 1 || got[0] != "https://skills.sh/owner/tdd/tdd" {
		t.Errorf("应打开 skills.sh 上的技能页，得到 %v", got)
	}
}

// TestMarketLinksEscapePathSegments 验证外部链接逐段转义：远端给的技能标识里即使有空格、
// 问号或斜杠，也不能改写链接的路径。
func TestMarketLinksEscapePathSegments(t *testing.T) {
	sk := market.Skill{Source: "owner/repo", SkillID: "a b?c/../d"}
	if got := marketSkillsShURL(sk); got != "https://skills.sh/owner/repo/a%20b%3Fc%2F..%2Fd" {
		t.Errorf("skills.sh 链接没有逐段转义：%s", got)
	}
	if got := marketGitHubURL(market.Skill{Source: "me/tools"}); got != "https://github.com/me/tools" {
		t.Errorf("GitHub 链接 = %s", got)
	}
}

// shotSkillMD 是截图用的 SKILL.md 样例（frontmatter 保持原样，正文逐行可译）。
const shotSkillMD = "---\nname: vercel-react-best-practices\ndescription: React and Next.js performance guidance from Vercel engineering.\n---\n\n# React Best Practices\n\nFollow these rules when writing React components.\n\n## Avoid waterfalls\n\n- Start independent requests in parallel.\n- Prefer server components for data.\n\n```tsx\nconst [user, posts] = await Promise.all([getUser(), getPosts()])\n```\n"

// shotZH 是截图里译文用的词条（按整行查，没有词条的行原样保留）。
var shotZH = map[string]string{
	"React and Next.js performance guidance from Vercel engineering.": "来自 Vercel 工程团队的 React 与 Next.js 性能指引。",
	"# React Best Practices":                            "# React 最佳实践",
	"Follow these rules when writing React components.": "编写 React 组件时遵循这些规则。",
	"## Avoid waterfalls":                               "## 避免请求瀑布",
	"- Start independent requests in parallel.":         "- 并行发起彼此独立的请求。",
	"- Prefer server components for data.":              "- 数据获取优先使用服务端组件。",
}

// shotTranslate 按行查表翻译，模拟一个真实的译文（截图用，不联网）。
func shotTranslate(_ context.Context, _ store.Settings, text string) (string, error) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if zh, ok := shotZH[l]; ok {
			lines[i] = zh
		}
	}
	return strings.Join(lines, "\n"), nil
}

// TestRenderMarketShot 真实渲染技能市场的几个画面并存成 PNG（照 install_discover_test.go 的模式）：
// 设 KURA_UI_SHOTS=<绝对目录> 时才跑，CI 跳过。数据是假的，不联网。产物：market-grid.png（卡片网格）、
// market-list.png（列表）、market-detail.png（详情，原文）、market-detail-translated.png（详情，译文）。
func TestRenderMarketShot(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过技能市场截图")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	a, f, _ := installMarketApp(t)
	seedSkill(t, a.cfg, "tdd", nil)
	var skills []market.Skill
	for i, n := range []string{"vercel-react-best-practices", "web-design-guidelines", "remotion-best-practices",
		"tdd", "pdf", "frontend-design", "find-skills", "code-review"} {
		skills = append(skills, mkSkill(n, int64(90000-i*9000)))
	}
	f.set(market.ViewAllTime, 0, okPage(true, skills...))
	const sample = "vercel-react-best-practices"
	f.setDoc("owner/"+sample, sample, docResp{doc: market.Document{
		Description: "React and Next.js performance guidance from Vercel engineering.",
		SkillMD:     shotSkillMD,
		Files:       []string{"SKILL.md", "rules/async-parallel.md", "rules/bundle-size.md", "rules/server.md"},
	}})
	a.translate = shotTranslate
	a.settings = trReadySettings()
	tt := showApp(a, 1140, 760)
	click(t, a, tt, locales.UiActionMarket())
	saveFrame(t, tt, filepath.Join(out, "market-grid.png"))
	tt.SetDark(true)
	saveFrame(t, tt, filepath.Join(out, "market-grid-dark.png"))
	tt.SetSize(880, 640)
	saveFrame(t, tt, filepath.Join(out, "market-grid-narrow-dark.png"))
	tt.SetDark(false)
	saveFrame(t, tt, filepath.Join(out, "market-grid-narrow.png"))
	tt.SetSize(1140, 760)

	click(t, a, tt, locales.UiViewList())
	saveFrame(t, tt, filepath.Join(out, "market-list.png"))
	click(t, a, tt, locales.UiViewGrid())

	click(t, a, tt, sample)
	saveFrame(t, tt, filepath.Join(out, "market-detail.png"))
	tt.SetDark(true)
	saveFrame(t, tt, filepath.Join(out, "market-detail-dark.png"))
	tt.SetDark(false)

	click(t, a, tt, locales.UiTranslateTranslated())
	saveFrame(t, tt, filepath.Join(out, "market-detail-translated.png"))
}

// saveFrame 推进虚拟时钟，让页面入场与悬停动画完成，再把稳定帧存成 PNG。
// Tester 的时间不会随 Frame 自行前进，直接截帧会拍到入场时透明的页面。
func saveFrame(t *testing.T, tt *myui.Tester, path string) {
	t.Helper()
	tt.Advance(250 * time.Millisecond)
	fh, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if err := png.Encode(fh, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestMarketGridColsFitsWidth 验证卡片列数随宽度变：放得下几张就排几列，至少一列，宽到
// 放不下就不超过上限之外多排。
func TestMarketGridColsFitsWidth(t *testing.T) {
	cases := []struct {
		width float32
		want  int
	}{
		{0, 1}, {200, 1}, {500, 2}, {1100, 4}, {2000, 8},
	}
	for _, tc := range cases {
		if got := marketGridCols(tc.width, 12); got != tc.want {
			t.Errorf("marketGridCols(%v) = %d，想要 %d", tc.width, got, tc.want)
		}
	}
}

// TestMarketTranslationKeysStayApart 验证市场的译文 key 与本地技能的 key 互不相同：同名技能
// （来自别的仓库）不能共用一份译文。
func TestMarketTranslationKeysStayApart(t *testing.T) {
	sk := market.Skill{Source: "owner/tdd", Name: "tdd"}
	other := market.Skill{Source: "someone/else", Name: "tdd"}
	keys := []string{marketDescKey(sk), marketDocKey(sk), marketDescKey(other)}
	for _, local := range []string{cardDescKey("tdd"), detailDescKey("tdd"), docTransKey("tdd")} {
		for _, k := range keys {
			if k == local {
				t.Errorf("市场译文 key %q 与本地 key 相同", k)
			}
		}
	}
	if keys[0] == keys[2] {
		t.Error("同名但来源不同的技能不应共用译文 key")
	}
}

// TestMarketListModeKeepsRows 验证排列偏好切到列表后，条目仍是两行的列表，名字与安装按钮都在。
func TestMarketListModeKeepsRows(t *testing.T) {
	a, f, _ := installMarketApp(t)
	a.viewMode = store.ViewList
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 9), mkSkill("pdf", 5)))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())
	for _, want := range []string{"owner/tdd", "owner/pdf"} {
		if !tt.HasText(want) {
			t.Errorf("列表模式应显示 %q，文本 %v", want, tt.Texts())
		}
	}
}

// TestMarketViewSwitchPersistsPreference 点市场页面里的排列开关：偏好立刻换，并写进 config.json
// 的 view（与技能区共用同一个偏好）。
func TestMarketViewSwitchPersistsPreference(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 9)))
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())

	click(t, a, tt, locales.UiViewList())
	if a.viewMode != store.ViewList {
		t.Fatalf("点列表后排列应为列表，viewMode=%q", a.viewMode)
	}
	lib, err := store.LoadLibrary(a.cfg)
	if err != nil || lib.View != store.ViewList {
		t.Errorf("偏好应写进 config.json：view=%q err=%v", lib.View, err)
	}
}

// marketTranslateApp 造一个市场详情页已读到快照的应用：描述与 SKILL.md 都有内容，翻译
// 设置按 ready 填好或留空，翻译换成 fakeTranslator（绝不联网）。
func marketTranslateApp(t *testing.T, ready bool) (*App, market.Skill, *fakeTranslator) {
	t.Helper()
	a, f, _ := installMarketApp(t)
	tr := &fakeTranslator{}
	a.translate = tr.translate
	if ready {
		a.settings = trReadySettings()
	}
	sk := mkSkill("tdd", 9)
	f.setDoc(sk.Source, sk.Name, docResp{doc: market.Document{
		Description: "Write tests first", SkillMD: "# TDD\n\nRed, green, refactor.", Files: []string{"SKILL.md"},
	}})
	a.OpenMarketDetail(sk)
	a.flush()
	return a, sk, tr
}

// TestMarketTranslationTranslatesDescriptionAndSkillMD 译文模式开着、翻译已配好：描述与 SKILL.md
// 都进译文缓存（键与本地技能分开），显示时读译文。
func TestMarketTranslationTranslatesDescriptionAndSkillMD(t *testing.T) {
	a, sk, tr := marketTranslateApp(t, true)
	a.SetTranslateOn(true)
	a.flush()

	if got := a.trans.text(marketDescKey(sk), "Write tests first"); got != "[译]Write tests first" {
		t.Errorf("描述译文 = %q", got)
	}
	e, ok := a.trans.get(marketDocKey(sk), "# TDD\n\nRed, green, refactor.")
	if !ok || e.err != "" || !strings.HasPrefix(e.dst, "[译]") {
		t.Errorf("SKILL.md 译文缺失或失败：ok=%v entry=%+v", ok, e)
	}
	if tr.calls < 2 {
		t.Errorf("应至少发起描述与全文两次翻译，实际 %d 次", tr.calls)
	}
}

// TestMarketTranslationNeedsSettings 翻译 API 没配好时，打开译文模式不发任何请求；界面给引导。
func TestMarketTranslationNeedsSettings(t *testing.T) {
	a, _, tr := marketTranslateApp(t, false)
	a.SetTranslateOn(true)
	a.flush()
	if tr.calls != 0 {
		t.Errorf("未配置时不应发请求，实际 %d 次", tr.calls)
	}
}

// TestMarketTranslationRetriesAfterFailure 失败不会每帧重试；切到原文再切回译文才重试。
func TestMarketTranslationRetriesAfterFailure(t *testing.T) {
	a, sk, tr := marketTranslateApp(t, true)
	tr.err = errors.New("boom")
	a.SetTranslateOn(true)
	a.flush()
	if e, _ := a.trans.get(marketDescKey(sk), "Write tests first"); e.err == "" {
		t.Fatalf("失败应被记下：%+v", e)
	}
	calls := tr.calls
	a.flush()
	if tr.calls != calls {
		t.Errorf("失败后不应自动重试，调用次数从 %d 变成 %d", calls, tr.calls)
	}

	tr.err = nil
	a.SetTranslateOn(false)
	a.SetTranslateOn(true)
	a.flush()
	if got := a.trans.text(marketDescKey(sk), "Write tests first"); got != "[译]Write tests first" {
		t.Errorf("切回译文后应重试成功，得到 %q", got)
	}
}

// TestMarketTranslationDropsStaleResult 翻译途中配置变了（epoch 递增）：迟到的结果不写进缓存。
func TestMarketTranslationDropsStaleResult(t *testing.T) {
	a, sk, _ := marketTranslateApp(t, true)
	var pending []func()
	a.spawn = func(fn func()) { pending = append(pending, fn) }
	a.SetTranslateOn(true)
	a.trans.reset() // 等同于设置变了：清缓存、换 epoch
	for _, fn := range pending {
		fn()
	}
	a.flush()
	if _, ok := a.trans.get(marketDescKey(sk), "Write tests first"); ok {
		t.Error("配置变了以后，旧的译文结果不应写进缓存")
	}
}

// TestMarketDetailTranslateSwitchFrame 真实点击详情页里的「译文」：描述显示译文，且译文说明出现。
func TestMarketDetailTranslateSwitchFrame(t *testing.T) {
	a, f, _ := installMarketApp(t)
	tr := &fakeTranslator{}
	a.translate = tr.translate
	a.settings = trReadySettings()
	f.set(market.ViewAllTime, 0, okPage(false, mkSkill("tdd", 1234)))
	f.setDoc("owner/tdd", "tdd", docResp{doc: market.Document{
		Description: "Write tests first", SkillMD: "# Test Driven Skill",
	}})
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiActionMarket())
	click(t, a, tt, "tdd")

	click(t, a, tt, locales.UiTranslateTranslated())
	if !tt.HasText("[译]Write tests first") {
		t.Errorf("点「译文」后描述应显示译文，文本 %v", tt.Texts())
	}
}
