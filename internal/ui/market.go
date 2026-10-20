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

// 市场页面的左右留白分两层：外层 gutterX 把滚动条从窗口边缘往里让一点（滚动条画在
// 滚动区最右侧 12 DIP 内），内层 padX 给内容留出可读的行宽。两层之和比本地三栏的
// Space(5) 宽，滚动条也就落在留白槽里，不会压在正文上。
const gutterX, padX = 2, 10

// marketGutter 返回外层的左右留白（DIP）。
func marketGutter(t *myui.Theme) float32 { return t.Space(gutterX) }

// marketPad 返回内容的左右留白（DIP）。
func marketPad(t *myui.Theme) float32 { return t.Space(padX) }

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
	grid   myui.ListState               // 卡片网格的虚拟行位置（与列表分开记）
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
				a.selectInstalled(res.ID)
				a.filter, a.query = filterAll, ""
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
	// 左右留白给到滚动区里的内容（见 viewMarketDetail 里的说明）：滚动条画在滚动区
	// 最右侧 12 DIP 内，内容铺满就会压到它。表格以外的段落各自带上同一份留白。
	pad := marketPad(t)
	page := myui.Column(c.Key("market-list")).Grow(1).MinWidth(0).MinHeight(0).FillWidth().
		Background(a.pal.canvas).Padding(t.Space(4), marketGutter(t), t.Space(3)).Gap(t.Space(4))
	enterMotion(c, page)
	page.Children(func() {
		myui.Column(c).FillWidth().Padding(0, pad).Gap(t.Space(4)).Children(func() {
			myui.Row(c).FillWidth().Children(func() {
				marketBackButton(c, locales.UiMarketBackToSkills(), a.CloseMarket)
			})
			myui.Row(c).FillWidth().Gap(t.Space(4)).AlignItems(myui.Center).Children(func() {
				myui.Box(c).Size(t.Space(12), t.Space(12)).Shrink(0).Radius(t.Radius+4).
					Background(t.Accent.Alpha(0.10)).Border(1, t.Accent.Alpha(0.12)).Center().Children(func() {
					myui.Icon(c, iconGlobe).FontSize(t.FontSize + 10).TextColor(t.Accent)
				})
				myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
					myui.Text(c, locales.UiActionMarket()).FontWeight(600).FontSize(t.FontSize + 12).SingleLine()
					myui.Text(c, locales.UiMarketSubtitle(a.cfg.Root)).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
				})
			})
		})
		// 分段的点击在构建结束时才写进绑定的变量，所以用局部 sel 接选择，Changed 为真的
		// 那次构建里 sel 已是新值（同 viewModeSwitch）。排列开关与技能区共用一个偏好。
		myui.Box(c).FillWidth().Padding(0, pad).Children(func() {
			myui.Row(c).FillWidth().Padding(t.Space(2)).Radius(t.Radius+2).
				Background(a.pal.panel).Border(1, a.pal.line).AlignItems(myui.Center).Gap(t.Space(2)).Children(func() {
				sel := m.view
				if myui.Segmented(c, &sel, marketViewLabels()...).Label(locales.UiActionMarket()).Changed() {
					m.view = sel
					a.ensureMarketBoard()
				}
				myui.Spacer(c)
				a.viewModeSwitch(c)
			})
		})
		v := m.current()
		b := m.boardOr(v)
		a.viewMarketBoard(c, v, b)
		myui.Box(c).FillWidth().Padding(0, pad).Children(func() {
			myui.Text(c, locales.UiMarketNote()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
		if m.err != "" {
			myui.Box(c).FillWidth().Padding(0, pad).Children(func() {
				a.viewMarketError(c, m.err, nil)
			})
		}
		myui.Row(c).FillWidth().Padding(0, pad).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			a.viewMarketStatus(c)
			if b.loading && b.loaded {
				myui.Spinner(c)
			}
			if b.loaded && b.hasMore {
				more := myui.Button(c, locales.UiMarketLoadMore()).Disabled(b.loading)
				buttonMotion(c, more, !b.loading)
				if more.Clicked() {
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
		a.viewMarketItems(c, b)
	case b.loaded:
		myui.Column(c).Grow(1).FillWidth().Gap(t.Space(4)).AlignItems(myui.Center).Justify(myui.Center).Children(func() {
			myui.Box(c).Size(t.Space(16), t.Space(16)).Radius(t.Radius+8).
				Background(a.pal.panel).Border(1, a.pal.line).Center().Children(func() {
				myui.Icon(c, iconGlobe).FontSize(t.FontSize + 14).TextColor(t.Accent)
			})
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
	row := myui.Box(c.Key(marketKey(sk))).FillWidth().Padding(t.Space(3), t.Space(4)).Radius(t.Radius + 4)
	surfaceMotion(c, row, a.pal, false, false)
	row.Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(4)).AlignItems(myui.Center).Children(func() {
			info := myui.ButtonBase(c).Grow(1).MinWidth(0).Justify(myui.Start).Tooltip(sk.Name)
			info.Children(func() {
				myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
					avatar(c, a.pal, sk.Name, t.Space(9), false)
					myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
						myui.Text(c, sk.Name).SingleLine().FontWeight(600).MinWidth(0)
						myui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
							myui.Icon(c, iconGitHub).Shrink(0).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
							myui.Text(c, sk.Source).SingleLine().MinWidth(0).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
						})
					})
				})
			})
			if info.Clicked() {
				a.OpenMarketDetail(sk)
			}
			myui.Text(c, locales.UiMarketInstalls(int(sk.Installs))).SingleLine().Shrink(0).
				FontSize(t.FontSize - 2).TextColor(t.TextMuted)
			a.marketInstallAction(c, sk, installed[strings.ToLower(sk.Name)], false)
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
	// 左右留白加在滚动区里面（同本地详情页 viewDetail），不夹在滚动区外面：
	// 滚动条画在滚动区最右侧 12 DIP 内，内容铺满就会压到它。
	pad := marketPad(t)
	page := myui.Column(c.Key("market-detail")).Grow(1).MinWidth(0).MinHeight(0).FillWidth().Background(a.pal.canvas).
		Padding(t.Space(4), marketGutter(t), t.Space(3)).Gap(t.Space(4))
	enterMotion(c, page)
	page.Children(func() {
		myui.Row(c).FillWidth().Padding(0, pad).AlignItems(myui.Center).Children(func() {
			marketBackButton(c, locales.UiMarketBackToBoard(), a.CloseMarketDetail)
		})
		myui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(0, pad, t.Space(4)).Gap(t.Space(4)).Children(func() {
				a.viewMarketDetailHead(c, sk)
				a.viewTranslateBar(c)
				if d.loaded && d.doc.Description != "" {
					myui.Text(c, a.marketDescText(sk, d.doc.Description)).FontSize(t.FontSize + 1).TextColor(a.pal.textMid)
					a.viewMarketTranslateNote(c, sk, d.doc.Description)
				}
				myui.Row(c).FillWidth().Wrap().Gap(t.Space(2)).Children(func() {
					skillsLink := myui.Button(c, locales.UiMarketViewSkillsSh())
					buttonMotion(c, skillsLink, true)
					if skillsLink.Clicked() {
						a.openMarketURL(marketSkillsShURL(sk))
					}
					githubLink := myui.Button(c, locales.UiMarketViewGitHub())
					buttonMotion(c, githubLink, true)
					if githubLink.Clicked() {
						a.openMarketURL(marketGitHubURL(sk))
					}
				})
				if d.loaded && len(d.doc.Files) > 0 {
					myui.Column(c).FillWidth().Padding(t.Space(4)).Gap(t.Space(2)).
						Radius(t.Radius+2).Background(a.pal.panel).Border(1, a.pal.line).Children(func() {
						myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
							myui.Icon(c, iconFolder).FontSize(t.FontSize).TextColor(t.TextMuted)
							myui.Text(c, locales.UiMarketFiles(len(d.doc.Files))).FontSize(t.FontSize - 1).FontWeight(600).TextColor(a.pal.textMid)
						})
						myui.Text(c, strings.Join(d.doc.Files, "\n")).FontSize(t.FontSize - 1).TextColor(t.TextMuted).MaxLines(10)
					})
				}
				myui.Divider(c)
				a.viewMarketDoc(c, sk, d)
			})
		})
		myui.Row(c).FillWidth().Padding(0, pad).AlignItems(myui.Center).Children(func() {
			a.viewMarketStatus(c)
		})
	})
}

// viewMarketDetailHead 是详情页的头部：大头像、名字、来源与安装次数；右侧是安装按钮（或已安装的
// 标记、正在装的转圈）。
func (a *App) viewMarketDetailHead(c *myui.Context, sk market.Skill) {
	t := c.Theme()
	installed := a.installedNames()[strings.ToLower(sk.Name)]
	myui.Row(c).FillWidth().Padding(t.Space(5)).Gap(t.Space(4)).AlignItems(myui.Center).
		Radius(t.Radius+6).Background(a.pal.panel).Border(1, a.pal.line).Children(func() {
		avatar(c, a.pal, sk.Name, t.Space(14), false)
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
			myui.Text(c, sk.Name).FontWeight(600).FontSize(t.FontSize + 9).SingleLine()
			myui.Text(c, sk.Source).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Text(c, locales.UiMarketInstalls(int(sk.Installs))).SingleLine().FontSize(t.FontSize - 2).TextColor(t.TextMuted)
		})
		a.marketInstallAction(c, sk, installed, true)
	})
}

// marketInstallAction 让卡片、列表与详情共享安装反馈；安装按钮独立于详情入口，
// 动画只改变视觉，不把点击传给外层技能卡片。
func (a *App) marketInstallAction(c *myui.Context, sk market.Skill, installed, primary bool) {
	t := c.Theme()
	m := &a.market
	switch {
	case installed:
		pill(c, locales.UiMarketInstalled(), t.Success)
	case m.installing == marketKey(sk):
		myui.Spinner(c)
	default:
		var button myui.Element
		if primary {
			button = myui.PrimaryButton(c, locales.UiMarketInstall())
		} else {
			button = myui.Button(c, locales.UiMarketInstall())
		}
		enabled := m.installing == ""
		button.Disabled(!enabled).Shrink(0)
		buttonMotion(c, button, enabled)
		if button.Clicked() {
			a.InstallMarketSkillAsync(sk)
		}
	}
}

// viewMarketDoc 是 SKILL.md 全文区：读到了就渲染（沿用全文页的 Markdown 视图，会跳过
// frontmatter 并截断超大的源码）；读失败给原因与重试；没有 SKILL.md 说明一声；读取中转圈。
func (a *App) viewMarketDoc(c *myui.Context, sk market.Skill, d *marketDoc) {
	t := c.Theme()
	switch {
	case d.loaded && d.doc.SkillMD != "":
		a.viewMarketSkillMD(c, sk, d.doc.SkillMD)
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
	buttonMotion(c, b, true)
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
	myui.Box(c).FillWidth().Padding(t.Space(3), t.Space(4)).Radius(t.Radius+2).
		Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.24)).Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Text(c, text).Grow(1).MinWidth(0).MaxLines(3).TextColor(t.Danger)
			if retry != nil {
				button := myui.Button(c, locales.UiMarketRetry()).Shrink(0)
				buttonMotion(c, button, true)
				if button.Clicked() {
					retry()
				}
			}
		})
	})
}

// marketCardMinW 是卡片网格里一张卡片的最小宽度（DIP）：窗口更宽就多排一列，卡片不会被
// 拉得过宽。
const marketCardMinW = 230

// marketGridCols 按可用宽度算每行能放几张卡片，至少一张。
func marketGridCols(width, gap float32) int {
	return max(int((width+gap)/(marketCardMinW+gap)), 1)
}

// viewMarketItems 是榜单的条目区，按排列偏好（store.ViewGrid 卡片网格 / 其余为列表）渲染。
// 两种排列都是虚拟的，只构建看得见的行；各自的滚动位置分别记在 marketState.grid 与 list 里。
func (a *App) viewMarketItems(c *myui.Context, b *marketBoard) {
	if a.viewMode == store.ViewGrid {
		a.viewMarketGrid(c, b)
		return
	}
	installed := a.installedNames()
	pad := marketPad(c.Theme())
	myui.List(c, &a.market.list, len(b.skills), func(i int) {
		myui.Box(c).FillWidth().Padding(c.Theme().Space(1), pad, c.Theme().Space(1)).Children(func() {
			a.viewMarketRow(c, b.skills[i], installed)
		})
	}).Grow(1).MinHeight(0)
}

// viewMarketGrid 是卡片网格：每个虚拟行放 cols 张卡片，cols 随窗口宽度变。最后一行不满的
// 地方用空框占位，卡片宽度保持一致。
func (a *App) viewMarketGrid(c *myui.Context, b *marketBoard) {
	t := c.Theme()
	w, _ := c.Size()
	gap := t.Space(3)
	cols := marketGridCols(w-2*(marketGutter(t)+marketPad(t)), gap)
	rows := (len(b.skills) + cols - 1) / cols
	installed := a.installedNames()
	pad := marketPad(t)
	myui.List(c, &a.market.grid, rows, func(r int) {
		// 虚拟行本身没有间隔；留出垂直槽，避免相邻卡片边框相接，也容纳悬停轻抬。
		myui.Box(c).FillWidth().Padding(t.Space(1), pad, t.Space(2)).Children(func() {
			myui.Row(c).FillWidth().Gap(gap).AlignItems(myui.Stretch).Children(func() {
				for k := 0; k < cols; k++ {
					if i := r*cols + k; i < len(b.skills) {
						a.viewMarketCard(c, b.skills[i], installed)
					} else {
						myui.Box(c).Grow(1).Basis(0).MinWidth(0)
					}
				}
			})
		})
	}).Grow(1).MinHeight(0)
}

// viewMarketCard 是网格里的一张卡片：头像、名字与来源（点它进详情页）；底部是安装次数与安装
// 按钮（已装过显示「已安装」，正在装的转圈）。卡片不放描述：榜单接口不给描述，描述只在详情页
// 读快照之后才有。
func (a *App) viewMarketCard(c *myui.Context, sk market.Skill, installed map[string]bool) {
	t := c.Theme()
	card := myui.Box(c.Key(marketKey(sk))).Grow(1).Basis(0).MinWidth(0).Padding(t.Space(4)).
		Radius(t.Radius+4).Shadow(0, 2, 8, 0, a.pal.shadow)
	surfaceMotion(c, card, a.pal, false, true)
	card.Children(func() {
		myui.Column(c).FillWidth().Gap(t.Space(4)).Children(func() {
			info := myui.ButtonBase(c).FillWidth().MinWidth(0).Justify(myui.Start).Tooltip(sk.Name)
			info.Children(func() {
				myui.Column(c).FillWidth().MinWidth(0).Gap(t.Space(3)).Children(func() {
					avatar(c, a.pal, sk.Name, t.Space(10), false)
					myui.Column(c).FillWidth().MinWidth(0).Gap(t.Space(1)).Children(func() {
						myui.Text(c, sk.Name).SingleLine().FontWeight(600).MinWidth(0).FontSize(t.FontSize + 1)
						myui.Row(c).FillWidth().Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
							myui.Icon(c, iconGitHub).Shrink(0).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
							myui.Text(c, sk.Source).SingleLine().MinWidth(0).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
						})
					})
				})
			})
			if info.Clicked() {
				a.OpenMarketDetail(sk)
			}
			myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
				myui.Text(c, locales.UiMarketInstalls(int(sk.Installs))).SingleLine().Grow(1).MinWidth(0).
					FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				a.marketInstallAction(c, sk, installed[strings.ToLower(sk.Name)], false)
			})
		})
	})
}

// marketDescKey 是市场技能描述的译文 key。带 market: 前缀：同名的本地技能（来自别的仓库）
// 不会共用一份译文。
func marketDescKey(sk market.Skill) string {
	return "market:" + sk.Source + "/" + sk.Name + translateDescSuffix
}

// marketDocKey 是市场技能 SKILL.md 全文译文的 key（同样避开本地技能的 doc: 键）。
func marketDocKey(sk market.Skill) string {
	return "mdoc:" + sk.Source + "/" + sk.Name + translateDocSuffix
}

// marketDescText 返回市场详情里描述该显示的文本：译文模式下用缓存的译文，拿不到退回原文。
// 只读缓存，绝不发请求（同 App.descText）。
func (a *App) marketDescText(sk market.Skill, desc string) string {
	if !a.trans.on || strings.TrimSpace(desc) == "" {
		return desc
	}
	return a.trans.text(marketDescKey(sk), desc)
}

// viewMarketTranslateNote 是市场描述下方的一行小字，同 viewTranslateNote：翻译中 / 失败原因
// / 当前译文的语言。翻译 API 未配置时不说话（引导在开关条里）。
func (a *App) viewMarketTranslateNote(c *myui.Context, sk market.Skill, desc string) {
	if !a.trans.on || !a.settings.Ready() || strings.TrimSpace(desc) == "" {
		return
	}
	t := c.Theme()
	e, ok := a.trans.get(marketDescKey(sk), desc)
	switch {
	case !ok || e.pending:
		myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, locales.UiTranslateInProgress()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
	case e.err != "":
		myui.Text(c, locales.UiTranslateFailed(e.err)).MaxLines(4).FontSize(t.FontSize - 1).TextColor(t.Danger)
	default:
		myui.Text(c, locales.UiTranslateNote(a.settings.LangLabel())).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
	}
}

// viewMarketSkillMD 渲染市场技能的 SKILL.md 全文。译文模式下的做法同 viewFullTextBody：译文还在
// 路上先显示原文，顶上一行「翻译全文中…」；失败说明原因，仍显示原文；译文到了替换成译文。
func (a *App) viewMarketSkillMD(c *myui.Context, sk market.Skill, src string) {
	t := c.Theme()
	if !a.trans.on || !a.settings.Ready() || strings.TrimSpace(src) == "" {
		a.viewMarkdown(c, src)
		return
	}
	e, ok := a.trans.get(marketDocKey(sk), src)
	switch {
	case !ok || e.pending:
		myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, locales.UiTranslateFullInProgress()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
		a.viewMarkdown(c, src)
	case e.err != "":
		myui.Text(c, locales.UiTranslateFullFailed(e.err)).MaxLines(4).FontSize(t.FontSize - 1).TextColor(t.Danger)
		a.viewMarkdown(c, src)
	default:
		myui.Text(c, locales.UiTranslateNote(a.settings.LangLabel())).FontSize(t.FontSize - 2).TextColor(t.TextMuted)
		a.viewMarkdown(c, e.dst)
	}
}

// ensureMarketTranslations 为市场详情页里的技能发起翻译（若需要）：译文模式开着、翻译 API 已就绪，
// 且描述与 SKILL.md 还没有译文。与 ensureTranslations 一样受缓存占位保护（同一原文只发一次，失败
// 不在每帧重试），每帧调用无妨。只在市场详情页生效。
func (a *App) ensureMarketTranslations() {
	if a.market.screen != marketDetail || !a.trans.on || !a.settings.Ready() {
		return
	}
	sk := a.market.detail
	d := a.market.docOr(marketKey(sk))
	if !d.loaded {
		return
	}
	if strings.TrimSpace(d.doc.Description) != "" {
		a.TranslateAsync(marketDescKey(sk), d.doc.Description)
	}
	a.TranslateDocAsync(marketDocKey(sk), d.doc.SkillMD)
}

// ensureCurrentTranslations 为当前画面需要的文本发起翻译：市场详情页看市场技能，其余时候看选中的
// 本地技能。View 每帧与切换译文模式时都调用，两条路径都受缓存占位保护。
func (a *App) ensureCurrentTranslations() {
	if a.market.screen != marketNone {
		a.ensureMarketTranslations()
		return
	}
	a.ensureTranslations(a.selected())
}
