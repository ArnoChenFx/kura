package ui

// 技能市场页面：浏览 skills.sh 的榜单（总榜 / 热度 / 趋势），点开一个技能看它的
// 详情（描述、文件、SKILL.md 全文、两个外部链接），并把它装进当前技能目录。
// 页面替换三栏的内容区（标题栏仍在），「返回技能目录」回到三栏。
//
// 读榜单、读快照、安装与打开外部链接都在后台执行，结果经 publish 回到主线程
// （同安装弹窗）。每个榜单、每个技能的快照各自记住读到的内容，切回来不重读；
// 失败可以重试。这些内容只在内存里，不落盘，也不写进任何技能目录。

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// marketViews 是分段的顺序（下标即 marketState.view）。总榜排第一并默认选中：它是
// 三个榜单里唯一按已知接口形状核对过的，见 docs/note/skills-market.md。
var marketViews = []market.View{market.ViewAllTime, market.ViewHot, market.ViewTrending}

// marketPageTimeout 是读一页榜单或一份快照的上限（客户端自己也有超时，这里是兜底）。
const marketPageTimeout = 25 * time.Second

// marketScreen 是市场所在的画面：marketNone 时显示三栏（本地技能）。
type marketScreen int

const (
	marketNone   marketScreen = iota
	marketList                // 榜单列表
	marketDetail              // 某个技能的详情
)

// marketBoard 是一个榜单已读到的内容。
type marketBoard struct {
	skills  []market.Skill
	next    int    // 下一页的页码：只有读成功一页才前进
	hasMore bool   // 服务器说后面还有
	loaded  bool   // 至少读成功过一页
	loading bool   // 正在读某一页（同一时间只读一页）
	err     string // 最近一次读取失败的原因
}

// marketDoc 是一个技能的快照读取状态（详情页用）。
type marketDoc struct {
	doc     market.Document
	loaded  bool
	loading bool
	err     string
}

// marketState 是技能市场的状态。
type marketState struct {
	screen marketScreen
	view   int                          // 分段下标，指向 marketViews
	boards map[market.View]*marketBoard // 每个榜单各自读到的内容
	list   myui.ListState               // 列表页虚拟列表的位置（只在 UI 线程读写）
	// installing 是正在安装的技能（marketKey），空为无；同一时间只装一个。
	installing string
	err        string // 最近一次安装失败的原因（列表页底部）
	detail     market.Skill
	docs       map[string]*marketDoc // 详情页读到的快照，键 marketKey
}

// board 返回榜单 v 的状态，第一次用到时创建。只在动作与发布回调里调用，渲染用 boardOr。
func (m *marketState) board(v market.View) *marketBoard {
	if m.boards == nil {
		m.boards = map[market.View]*marketBoard{}
	}
	b := m.boards[v]
	if b == nil {
		b = &marketBoard{}
		m.boards[v] = b
	}
	return b
}

// boardOr 返回榜单 v 的状态；还没有就返回一份空的（不存进 boards）。渲染用它，
// 构建路径不改应用状态。
func (m *marketState) boardOr(v market.View) *marketBoard {
	if b := m.boards[v]; b != nil {
		return b
	}
	return &marketBoard{}
}

// doc 返回技能 key 的快照状态，第一次用到时创建（只在动作里调用）。
func (m *marketState) doc(key string) *marketDoc {
	if m.docs == nil {
		m.docs = map[string]*marketDoc{}
	}
	d := m.docs[key]
	if d == nil {
		d = &marketDoc{}
		m.docs[key] = d
	}
	return d
}

// docOr 返回快照状态；没有就返回空的（不存进 docs）。渲染用它。
func (m *marketState) docOr(key string) *marketDoc {
	if d := m.docs[key]; d != nil {
		return d
	}
	return &marketDoc{}
}

// current 返回分段选中的榜单。
func (m *marketState) current() market.View {
	if m.view < 0 || m.view >= len(marketViews) {
		return marketViews[0]
	}
	return marketViews[m.view]
}

// marketViewLabels 是分段的文字，顺序与 marketViews 一致（每帧现取，语言一切就跟着变）。
func marketViewLabels() []string {
	return []string{locales.UiMarketViewAllTime(), locales.UiMarketViewHot(), locales.UiMarketViewTrending()}
}

// marketKey 标识榜单里的一个技能：同一个仓库里的同名技能只算一个。
func marketKey(sk market.Skill) string { return sk.Source + "\x00" + sk.Name }

// marketSkillsShURL 是技能在 skills.sh 上的页面（路径形如 /<owner>/<repo>/<skill>）。
// 每一段都转义：这些字段来自远端数据，交给系统浏览器前不能让它们改写路径。
func marketSkillsShURL(sk market.Skill) string {
	owner, repo, _ := strings.Cut(sk.Source, "/")
	return "https://skills.sh/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/" + url.PathEscape(sk.SkillID)
}

// marketGitHubURL 是技能来源仓库的 GitHub 页面（路径同样逐段转义）。
func marketGitHubURL(sk market.Skill) string {
	owner, repo, _ := strings.Cut(sk.Source, "/")
	return "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

// OpenMarket 打开技能市场页面：显示当前分段的榜单；没读到内容（包括上次读失败）
// 就读第一页。
func (a *App) OpenMarket() {
	a.market.screen, a.market.err = marketList, ""
	a.ensureMarketBoard()
}

// CloseMarket 回到三栏的本地技能目录。
func (a *App) CloseMarket() { a.market.screen = marketNone }

// OpenMarketDetail 打开技能的详情页，并读它的快照（没读过才读）。
func (a *App) OpenMarketDetail(sk market.Skill) {
	a.market.screen, a.market.detail = marketDetail, sk
	if d := a.market.doc(marketKey(sk)); !d.loaded && !d.loading {
		a.loadMarketDocAsync(sk)
	}
}

// CloseMarketDetail 从详情页回到榜单列表。
func (a *App) CloseMarketDetail() { a.market.screen = marketList }

// ensureMarketBoard 在当前榜单还没读到内容时读第一页。已读到内容或正在读的榜单不
// 重复发起；读失败的榜单在下一次打开或切到它时再读一次，失败原因留在页面里，
// 「重试」同样可用。只在动作（打开、切换分段）里调用。
func (a *App) ensureMarketBoard() {
	v := a.market.current()
	if b := a.market.board(v); !b.loaded && !b.loading {
		a.loadMarketPageAsync(v, 0)
	}
}

// loadMarketPageAsync 在后台读榜单 v 的第 page 页：page 为 0 时替换已有内容，
// 否则接在后面。同一榜单同一时间只读一页，读的途中再点不会重复发起。结果经
// publish 回到主线程再写进榜单状态。
func (a *App) loadMarketPageAsync(v market.View, page int) {
	b := a.market.board(v)
	if b.loading {
		return
	}
	b.loading, b.err = true, ""
	fetch := a.marketFetch
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), marketPageTimeout)
		defer cancel()
		p, err := fetch(ctx, v, page)
		a.publish(func() {
			b.loading = false
			if err != nil {
				b.err = err.Error()
				return
			}
			if page == 0 {
				b.skills = p.Skills
			} else {
				b.skills = append(b.skills, p.Skills...)
			}
			b.next, b.hasMore, b.loaded = page+1, p.HasMore, true
		})
	})
}

// loadMarketDocAsync 在后台读技能 sk 的快照（SKILL.md 与文件列表）。结果只回写快照
// 状态，与当前画面无关：切走再回来不会丢，也不会覆盖别的技能。
func (a *App) loadMarketDocAsync(sk market.Skill) {
	d := a.market.doc(marketKey(sk))
	if d.loading {
		return
	}
	d.loading, d.err = true, ""
	fetch := a.marketDocFetch
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), marketPageTimeout)
		defer cancel()
		res, err := fetch(ctx, sk.Source, sk.Name)
		a.publish(func() {
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.doc, d.loaded = res, true
		})
	})
}

// openMarketURL 在后台用系统浏览器打开链接（系统调用会阻塞，不在 UI 线程做）。
// 失败只在状态行报告。
func (a *App) openMarketURL(u string) {
	open := a.openURL
	a.spawn(func() {
		if err := open(u); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiMarketOpenFailed(err.Error()), true })
		}
	})
}

// InstallMarketSkillAsync 把技能装进当前技能目录。完成后重扫并选中新装的技能；失败时
// 页面保持打开并显示原因。与 startInstall 一样在发起时记下 cfg 与 gen：中途切换了
// 目录，旧目录的结果就不覆盖新目录的列表。
func (a *App) InstallMarketSkillAsync(sk market.Skill) {
	m := &a.market
	if m.installing != "" {
		return
	}
	m.installing, m.err = marketKey(sk), ""
	cfg, gen := a.cfg, a.gen
	a.status, a.statusErr = locales.UiMarketInstalling(sk.Name), false
	a.spawn(func() {
		res, err := cfg.AddSkillByName(sk.Source, sk.Name)
		var skills []*store.Skill
		var scanErr error
		if err == nil {
			skills, scanErr = cfg.ScanAll()
		}
		a.publish(func() {
			m.installing = ""
			if err != nil {
				m.err = err.Error()
				a.status, a.statusErr = locales.UiStatusInstallFailed(), true
				return
			}
			if gen == a.gen {
				a.applyScan(skills, scanErr)
				a.selName, a.filter, a.query = res.ID, filterAll, ""
			}
			a.status, a.statusErr = locales.UiStatusInstalled(res.ID), false
		})
	})
}

// installedNames 返回当前目录里启用中技能的名字（小写），用来判断「已安装」。停用与回收站里
// 的技能不在目录里，不算：安装写的是目录，它们不会挡住。
func (a *App) installedNames() map[string]bool {
	out := make(map[string]bool, len(a.skills))
	for _, s := range a.skills {
		if s.State == store.StateEnabled {
			out[strings.ToLower(s.Name)] = true
		}
	}
	return out
}

// viewMarketPage 是技能市场页面，替换三栏的内容区；画面由 marketState.screen 决定。
func (a *App) viewMarketPage(c *myui.Context) {
	if a.market.screen == marketDetail {
		a.viewMarketDetail(c)
		return
	}
	a.viewMarketList(c)
}

// viewMarketList 是榜单页：
//
//	[← 返回技能目录]  技能市场
//	                  安装到 <目录>
//	[总榜 | 热度 | 趋势]
//	┌────────────────────────────────────────────┐
//	│ 头像 名字                 1,234 次安装 [安装] │  ← 虚拟列表；点名字进详情
//	└────────────────────────────────────────────┘
//	（说明：榜单来自 skills.sh，安装从 GitHub 仓库下载）
//	状态 / 原因                          [加载更多]
func (a *App) viewMarketList(c *myui.Context) {
	m := &a.market
	t := c.Theme()
	myui.Column(c).Grow(1).MinWidth(0).MinHeight(0).FillWidth().Background(a.pal.canvas).
		Padding(t.Space(4), t.Space(5), t.Space(3), t.Space(5)).Gap(t.Space(3)).Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			marketBackButton(c, locales.UiMarketBackToSkills(), a.CloseMarket)
			myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
				myui.Text(c, locales.UiActionMarket()).Bold().FontSize(t.FontSize + 6).SingleLine()
				myui.Text(c, locales.UiMarketSubtitle(a.cfg.Root)).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
		})
		// 分段的点击在构建结束时才写进绑定的变量，所以用局部 sel 接选择，Changed 为真的
		// 那次构建里 sel 已是新值（同 viewModeSwitch）。
		sel := m.view
		if myui.Segmented(c, &sel, marketViewLabels()...).Label(locales.UiActionMarket()).Changed() {
			m.view = sel
			a.ensureMarketBoard()
		}
		v := m.current()
		b := m.boardOr(v)
		a.viewMarketBoard(c, v, b)
		myui.Text(c, locales.UiMarketNote()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		if m.err != "" {
			a.viewMarketError(c, m.err, nil)
		}
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			a.viewMarketStatus(c)
			if b.loading && b.loaded {
				myui.Spinner(c)
			}
			if b.loaded && b.hasMore {
				if myui.Button(c, locales.UiMarketLoadMore()).Disabled(b.loading).Clicked() {
					a.loadMarketPageAsync(v, b.next)
				}
			}
		})
	})
}

// viewMarketBoard 是榜单区：有内容时是虚拟列表，填满剩下的高度；读完了但没有条目是
// 空态；还没读到第一页是加载态。读失败的原因与「重试」由 viewMarketError 接管。
func (a *App) viewMarketBoard(c *myui.Context, v market.View, b *marketBoard) {
	t := c.Theme()
	switch {
	case len(b.skills) > 0:
		installed := a.installedNames()
		myui.List(c, &a.market.list, len(b.skills), func(i int) {
			a.viewMarketRow(c, b.skills[i], installed)
		}).Grow(1).MinHeight(0)
	case b.loaded:
		myui.Column(c).Grow(1).FillWidth().AlignItems(myui.Center).Justify(myui.Center).Children(func() {
			myui.Text(c, locales.UiMarketEmpty()).TextColor(t.TextMuted)
		})
	case b.err == "":
		myui.Row(c).Grow(1).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Justify(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, locales.UiMarketLoading()).TextColor(t.TextMuted)
		})
	}
	if b.err != "" {
		a.viewMarketError(c, locales.UiMarketLoadFailed(b.err), func() { a.loadMarketPageAsync(v, b.next) })
	}
}

// viewMarketRow 是榜单里的一行：左边是头像、名字与来源（点它进详情页），右边是安装次数
// 与安装按钮。本目录里已有同名启用技能时显示「已安装」；正在装的这一行显示转圈，其余行的
// 按钮在有安装进行时禁用（同一时间只装一个）。安装按钮与名字区是两个并列的控件，点安装
// 不会误开详情。
func (a *App) viewMarketRow(c *myui.Context, sk market.Skill, installed map[string]bool) {
	t := c.Theme()
	m := &a.market
	myui.Box(c).FillWidth().Padding(t.Space(2), t.Space(2.5)).Radius(t.Radius+2).
		Background(a.pal.card).Border(1, a.pal.line).Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
			info := myui.ButtonBase(c).Grow(1).MinWidth(0).Justify(myui.Start)
			info.Children(func() {
				myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
					avatar(c, a.pal, sk.Name, t.Space(7), false)
					myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
						myui.Text(c, sk.Name).SingleLine().FontWeight(600).MinWidth(0)
						myui.Text(c, sk.Source).SingleLine().MinWidth(0).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
					})
				})
			})
			if info.Clicked() {
				a.OpenMarketDetail(sk)
			}
			myui.Text(c, locales.UiMarketInstalls(int(sk.Installs))).SingleLine().Shrink(0).
				FontSize(t.FontSize - 2).TextColor(t.TextMuted)
			switch {
			case installed[strings.ToLower(sk.Name)]:
				pill(c, locales.UiMarketInstalled(), t.Accent)
			case m.installing == marketKey(sk):
				myui.Spinner(c)
			default:
				if myui.Button(c, locales.UiMarketInstall()).Disabled(m.installing != "").Clicked() {
					a.InstallMarketSkillAsync(sk)
				}
			}
		})
	})
}

// viewMarketDetail 是技能详情页：顶部返回榜单；其余内容放在滚动区里：头部（头像、名字、
// 来源、安装次数、安装按钮）、描述、两个外部链接、文件列表，然后是 SKILL.md 全文。
func (a *App) viewMarketDetail(c *myui.Context) {
	m := &a.market
	t := c.Theme()
	sk := m.detail
	d := m.docOr(marketKey(sk))
	myui.Column(c).Grow(1).MinWidth(0).MinHeight(0).FillWidth().Background(a.pal.canvas).
		Padding(t.Space(4), t.Space(5), t.Space(3), t.Space(5)).Gap(t.Space(3)).Children(func() {
		myui.Row(c).FillWidth().AlignItems(myui.Center).Children(func() {
			marketBackButton(c, locales.UiMarketBackToBoard(), a.CloseMarketDetail)
		})
		myui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
				a.viewMarketDetailHead(c, sk)
				if d.loaded && d.doc.Description != "" {
					myui.Text(c, d.doc.Description).FontSize(t.FontSize).TextColor(t.TextMuted)
				}
				myui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
					if myui.Button(c, locales.UiMarketViewSkillsSh()).Clicked() {
						a.openMarketURL(marketSkillsShURL(sk))
					}
					if myui.Button(c, locales.UiMarketViewGitHub()).Clicked() {
						a.openMarketURL(marketGitHubURL(sk))
					}
				})
				if d.loaded && len(d.doc.Files) > 0 {
					myui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
						myui.Text(c, locales.UiMarketFiles(len(d.doc.Files))).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
						myui.Text(c, strings.Join(d.doc.Files, "\n")).FontSize(t.FontSize - 1).TextColor(t.TextMuted).MaxLines(10)
					})
				}
				myui.Divider(c)
				a.viewMarketDoc(c, sk, d)
			})
		})
		myui.Row(c).FillWidth().AlignItems(myui.Center).Children(func() {
			a.viewMarketStatus(c)
		})
	})
}

// viewMarketDetailHead 是详情页的头部：大头像、名字、来源与安装次数；右侧是安装按钮（或已安装的
// 标记、正在装的转圈）。
func (a *App) viewMarketDetailHead(c *myui.Context, sk market.Skill) {
	t := c.Theme()
	m := &a.market
	installed := a.installedNames()[strings.ToLower(sk.Name)]
	myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
		avatar(c, a.pal, sk.Name, t.Space(12), false)
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
			myui.Text(c, sk.Name).Bold().FontSize(t.FontSize + 5).SingleLine()
			myui.Text(c, sk.Source).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Text(c, locales.UiMarketInstalls(int(sk.Installs))).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
		switch {
		case installed:
			pill(c, locales.UiMarketInstalled(), t.Accent)
		case m.installing == marketKey(sk):
			myui.Spinner(c)
		default:
			if myui.PrimaryButton(c, locales.UiMarketInstall()).Disabled(m.installing != "").Clicked() {
				a.InstallMarketSkillAsync(sk)
			}
		}
	})
}

// viewMarketDoc 是 SKILL.md 全文区：读到了就渲染（沿用全文页的 Markdown 视图，会跳过
// frontmatter 并截断超大的源码）；读失败给原因与重试；没有 SKILL.md 说明一声；读取中转圈。
func (a *App) viewMarketDoc(c *myui.Context, sk market.Skill, d *marketDoc) {
	t := c.Theme()
	switch {
	case d.loaded && d.doc.SkillMD != "":
		a.viewMarkdown(c, d.doc.SkillMD)
	case d.loaded:
		myui.Text(c, locales.UiMarketDocEmpty()).TextColor(t.TextMuted)
	case d.err != "":
		a.viewMarketError(c, locales.UiMarketDocFailed(d.err), func() { a.loadMarketDocAsync(sk) })
	default:
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, locales.UiMarketDocLoading()).TextColor(t.TextMuted)
		})
	}
}

// marketBackButton 是页面顶部的返回按钮：箭头加文字（两级导航里要看得出回到哪一级）。
func marketBackButton(c *myui.Context, label string, back func()) {
	t := c.Theme()
	b := myui.ButtonBase(c).Label(label).Tooltip(label).Padding(t.Space(1.5), t.Space(2)).
		Radius(t.Radius).TextColor(t.TextMuted)
	if b.Hovered() {
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	b.Children(func() {
		myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Icon(c, iconArrowLeft).FontSize(t.FontSize + 1)
			myui.Text(c, label).FontSize(t.FontSize - 1)
		})
	})
	if b.Clicked() {
		back()
	}
}

// viewMarketStatus 是页面底部的状态文字（安装结果、打开链接的失败等），没有就占位。
func (a *App) viewMarketStatus(c *myui.Context) {
	t := c.Theme()
	if a.status == "" {
		myui.Spacer(c)
		return
	}
	color := t.TextMuted
	if a.statusErr {
		color = t.Danger
	}
	myui.Text(c, a.status).SingleLine().Grow(1).MinWidth(0).FontSize(t.FontSize - 1).TextColor(color)
}

// viewMarketError 是页面里的错误框：text 是整句原因；retry 为空时不给按钮。
func (a *App) viewMarketError(c *myui.Context, text string, retry func()) {
	t := c.Theme()
	myui.Box(c).FillWidth().Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
		Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.4)).Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Text(c, text).Grow(1).MinWidth(0).MaxLines(3).TextColor(t.Danger)
			if retry != nil && myui.Button(c, locales.UiMarketRetry()).Clicked() {
				retry()
			}
		})
	})
}
