// Package ui 实现 kura 的原生界面（基于 MyGo 的 ui 包），三栏布局：
// 左栏是技能目录列表（可添加任意文件夹），中栏是所选目录的技能卡片网格
// （搜索 + 状态筛选 + 卡片上的启用开关），右栏是所选技能的详情。
//
// 线程约定：App 的字段只在 UI 线程读写；所有磁盘 I/O 都通过 spawn
// 放到后台 goroutine，结果通过 update（生产环境为 Window.Update）
// 发布回主线程。测试可以注入同步版 spawn/update。
package ui

import (
	"cmp"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	"github.com/egoist/mygo"
	myui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// confirmState 是确认框的状态：移入回收站（purge 为假）或从回收站永久删除
// （purge 为真）共用一个框，由 purge 决定文案与最终动作。
type confirmState struct {
	open  bool
	name  string
	state store.State
	purge bool
}

// skillSelection 区分同一目录里不同状态的同名技能：重新安装后，旧副本可以
// 仍留在停用区或回收站。名字仍是选择的主标识，状态仅用于消除这种歧义。
type skillSelection struct {
	name  string
	state store.State
}

// visibleSkill 保存扫描快照的搜索键；输入变化时只过滤，排序和大小写转换只做一次。
type visibleSkill struct {
	skill                              *store.Skill
	name, description, display, source string
}

type visibleCache struct {
	skills []*store.Skill
	sorted []visibleSkill
	rows   []*store.Skill
	query  string
	filter int
	valid  bool
}

// App 是 kura 主窗口的状态。View 由 MyGo 在主线程反复调用，
// 从这些字段构建每一帧。
type App struct {
	lib                    *store.Library               // 全部技能目录
	libErr                 string                       // 读取/保存目录列表的错误（config.json 损坏等）
	cfg                    *store.Config                // 当前选中目录的配置
	rootSel                string                       // 左栏选中的目录，与 cfg.Root 不同时切换
	gen                    int                          // 目录切换代数：切换后丢弃旧目录的后台结果
	rootCounts             map[string]int               // 每个目录根下在用技能数（左栏徽标）
	rootBrowsers           map[string]*rootBrowserState // 每个目录本次会话中的浏览位置，不写入配置
	restoringRootSelection bool                         // 等待扫描验证恢复的精确副本，不能误选同名另一状态
	adding                 bool                         // 正在选择/添加文件夹
	// rootsW 与 detailW 是用户拖出来的左栏与详情栏宽度偏好（DIP），由两侧的
	// 分割线拖动改变；窗口放不下时并不改它们，而是每帧按可用宽度夹紧出实际
	// 宽度 rootsEff / detailEff（见 fitWidths），窗口再变宽时偏好自然恢复。
	// 中栏占剩余宽度。bodyW 是本帧窗口内容的宽度（c.Size）。都只在内存里，
	// 重启后回到默认宽度。
	rootsW, detailW     float32
	rootsEff, detailEff float32
	bodyW               float32
	win                 *mygo.Window
	path                string // 导入元数据用的 .skill-lock.json 路径，空为默认位置

	// pickFolder 弹出系统的文件夹选择框，返回所选路径（取消为空串）。
	// 会阻塞，只在后台 goroutine 里调用；测试里替换为假实现。
	pickFolder func() (string, error)
	// openPath 用系统默认方式打开文件或文件夹，reveal 在文件管理器里显示并
	// 选中它，setSource 把外观模式同步给 MyGo（原生标题栏、系统菜单）。
	// 都会阻塞，只在后台 goroutine 里调用（SyncTheme 例外，见该方法）；
	// 测试里替换为假实现，绝不真的打开窗口。
	openPath  func(path string) error
	reveal    func(path string)
	setSource func(mode string)

	// checkUpdates 打开 MyGo 的原生更新窗口、检查 kura 应用自身的更新
	// （生产实现是 selfupdate.CheckForUpdates，由 main.go 在窗口就绪后
	// 注入；依赖更新插件已被挂载，未挂载时调用会 panic）。默认 nil：
	// NewApp 不填——测试直接建 App，绝不能真的触发更新检查；只有
	// main.go 里 SetWindow 之后才接线。nil 时按钮点下只在状态行报告。
	checkUpdates func()
	// autoCheck 是「自动检查更新」的当前值，界面线程上的副本，启动时取自
	// config.json（见 NewApp）。updateFound 是后台查到有新版本、左栏「检查更新」
	// 按钮该亮蓝点。
	autoCheck   bool
	updateFound bool
	// autoCheckSet 通知后台检查偏好变了（生产实现是 selfupdate.Background 的
	// SetAutomaticChecks），在写盘之后调用；与 checkUpdates 一样只在 main.go
	// 里接线，未接线时 nil，开关照常写 config.json。
	autoCheckSet func(bool)
	autoCheckSeq atomic.Int64 // 开关切换的序号：后台任务只执行最新一次
	autoCheckMu  sync.Mutex   // 串行化写盘与通知：最新的值总是最后生效

	skills   []*store.Skill   // 当前目录的全部技能（启用 + 停用 + 回收站）
	query    string           // 搜索词
	filter   int              // 状态筛选：filterAll/filterEnabled/filterDisabled/filterTrashed
	selName  string           // 选中的技能名，空为未选中；以名字为准，跨筛选保持
	selState store.State      // 同名技能并存时，选择属于哪个状态；移动后跟随仍存在的同名技能
	selSeq   uint64           // 选择变更代数：后台移动不能覆盖用户后来的选择
	selIdx   int              // 网格的选中下标（绑定 GridState.Selected），由 selName 推出
	syncIdx  int              // 上一次 syncSelection 写入 selIdx 的值：不同即网格里点了别的卡片
	shown    []skillSelection // 上一次构建网格时各卡片的身份，用来还原点击
	grid     *myui.GridState  // 当前目录独立持有的卡片网格状态
	list     *myui.ListState  // 当前目录独立持有的列表状态
	updates  map[string]bool  // 最近一次检查发现有更新的技能（当前目录）
	// 扫描产生的 Skill 是只读快照；指针序列副本也能识别测试直接替换 skills。
	visibleRows visibleCache
	skillsSeq   uint64 // 扫描结果发布的代数，防止更早的刷新盖掉操作后的列表或错误

	// 详情栏的「概览/全文」tab：detailTab 是当前选中的下标（0=概览，
	// 1=全文），detailSel 记录 tab 属于哪个技能：viewDetail 发现换人时
	// 把 detailTab 重置回 0，避免看到上一个技能的全文。
	detailTab int
	detailSel string
	// skillDocs 缓存每个技能读出的 SKILL.md（键：状态 + 技能名）：切 tab、来回
	// 切技能不必反复读盘；扫描后失效（applyScan），更新/改文件后的下一帧
	// 会重新读。docGen 随失效递增，后台读盘任务只回写同一代的缓存。
	skillDocs map[string]fullTextDoc
	docGen    int
	// md 是全文页（SKILL.md）的 Markdown 视图选项，交给官方 ui/markdown。
	// 官方视图把解析状态放在 ui.Local 里，但 Options 是应用持有的指针，必须
	// 跨帧稳定，所以存在这里，不能每帧在栈上造一个新 Options 再取地址。
	// mdTheme 是上一次给 md.Theme 用的窗口主题：Theme 只在它变了（外观开关
	// 或被跟随的系统外观变了）时重建，不每帧新建——原因与实测数据见
	// fulltext.go 的 syncMarkdownTheme。
	md      markdown.Options
	mdTheme myui.Theme

	busy           map[string]bool    // 正在执行操作的技能（busyKey）
	checking       string             // 正在预览更新的技能名（空为无）
	checkingAll    bool               // 正在全部检查
	checkCancel    context.CancelFunc // 取消旧目录或用户停止的只读更新检查
	checkSeq       uint64             // 旧检查的发布不能清掉新任务状态
	checkingUpdate bool               // 正在检查 kura 应用自身的更新（按钮防抖）
	scanning       bool               // 正在扫描当前目录；其他目录计数不占用刷新按钮
	scanGen        int                // scanning 属于哪个目录代数
	scanSeq        uint64             // 扫描请求序号，旧列表与旧计数结果不能覆盖新请求
	loadErr        string             // 扫描/环境错误（如根目录不存在）
	status         string             // 底部状态行的文字
	statusErr      bool               // 状态行是否为错误
	confirm        confirmState       // 待确认的删除操作
	install        installState       // 安装技能弹窗
	pal            palette            // 本帧的表面色（由 themeMode 决定浅色/深色）
	skin           *skin              // 本帧的皮肤；卡片网格用它的 cell 主题
	themeMode      string             // 外观偏好：store.ThemeSystem / ThemeLight / ThemeDark
	themeSeq       atomic.Int64       // 外观切换的序号：后台任务只执行最新一次
	themeMu        sync.Mutex         // 串行化同步原生外观与写 config.json
	viewMode       string             // 技能区排列模式：store.ViewGrid / store.ViewList
	viewSeq        atomic.Int64       // 排列模式切换的序号：后台任务只执行最新一次
	langPref       string             // 界面语言偏好：store.LangSystem / LangZH / LangEN
	langSeq        atomic.Int64       // 语言切换的序号：后台任务只执行最新一次
	// setLocale 把语言偏好应用到「应用显示的语言」。生产环境交给 mygo.App
	// （它同时重绘窗口、更新菜单、通知窗口与页面）；测试里 mygo 的事件循环
	// 没跑，换成 internal/i18n 的进程级覆盖（见 app_test.go 的 newAppFor）。
	setLocale   func(pref string)
	review      reviewState // 更新评审面板（Modal）
	history     *snapshotHistoryState
	snapshotSeq uint64

	// 技能市场（skills.sh 的榜单，见 market.go）：页面状态，以及读取榜单与快照的函数。
	// marketFetch 生产环境是 market.Client.List；测试注入假实现，绝不联网。
	market         marketState
	marketFetch    func(ctx context.Context, v market.View, page int) (market.Page, error)
	marketDocFetch func(ctx context.Context, source, name string) (market.Document, error)
	// openURL 用系统浏览器打开外部链接（技能详情页的两个链接），同样只在后台调用；
	// 测试里换成记录用的假实现。
	openURL func(url string) error

	// 翻译设置与译文（翻译只用于界面显示，绝不写技能目录，见 translate.go）。
	// settings 是 ~/.kura/settings.json 的内存副本：只在 UI 线程写，写时持
	// settingsMu。翻译任务在 UI 线程发起时拷走一份传给 translate，后台
	// goroutine 不直接读它。
	settings        store.Settings
	settingsLoaded  bool          // 设置读取成功（文件不存在也算，得到零值）
	settingsErr     string        // 读取 settings.json 的错误（JSON 损坏等）
	settingsMu      sync.RWMutex  // 保护 settings 在 UI 线程写与后台读之间的一致
	settingsSeq     atomic.Int64  // 保存设置的序号：后台任务只写最新一次
	settingsWriteMu sync.Mutex    // 串行化写 settings.json
	settingsUI      settingsState // 设置弹窗
	trans           *translateState
	// translate 用设置 s 调用翻译 API；生产实现走 s.Translate。设置由任务
	// 发起时在 UI 线程固定下来，同一任务的所有请求用同一套配置。测试注入假
	// 实现，绝不联网。只在后台 goroutine 里调用（会阻塞）。
	translate func(ctx context.Context, s store.Settings, text string) (string, error)

	// spawn 在后台运行 fn：生产环境为 go fn()。
	spawn func(func())
	// update 把 fn 调度到 UI 线程执行：生产环境为 Window.Update。
	update func(func())
	// pending 只在测试中缓存待执行的 update 闭包；生产环境里
	// SetWindow 之前的发布也暂存在这里，由 SetWindow 交给窗口回放。
	pending []func()
	// mu 保护 update/pending 在窗口接入（SetWindow）与后台发布间的一致。
	mu sync.Mutex
	// loaded 记录是否已做过首次扫描；首帧触发一次后台扫描。
	loaded bool
}

// NewApp 创建绑定 cfg 的应用状态。窗口创建后调用 SetWindow 接入
// Window.Update。
//
// cfg.Root 是默认技能目录；其余目录从 cfg.Home 下的 config.json 读取。
// 目录列表损坏不致命：只显示默认目录，并在界面上报告错误。
func NewApp(cfg *store.Config) *App {
	a := &App{selIdx: -1, syncIdx: -1, busy: map[string]bool{}, rootCounts: map[string]int{}, updates: map[string]bool{},
		rootsW: rootsWidth, detailW: detailWidth}
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		a.libErr = err.Error()
	}
	a.lib = lib
	a.autoCheck = lib.AutoCheck
	a.cfg = cfg
	a.rootSel = lib.Default()
	a.themeMode = lib.Theme
	a.viewMode = lib.View
	if a.viewMode == "" {
		a.viewMode = store.ViewGrid
	}
	// 界面语言偏好：具体语言的解析（system → zh/en）在 main.go / CLI 里做，
	// 窗口侧只记住偏好，切语言走 SetLangAsync。
	a.langPref = lib.Lang
	if a.langPref == "" {
		a.langPref = store.LangSystem
	}
	// 翻译设置：读取失败不致命（只报告），界面照常可用。
	if st, _, err := store.LoadSettings(cfg); err != nil {
		a.settingsErr = err.Error()
		// 旧配置损坏时保持未配置状态，不能静默把原先的自定义服务切到外部免费服务。
		a.settings.Provider = store.TranslationOpenAI
	} else {
		a.settings, a.settingsLoaded = *st, true
	}
	a.trans = newTranslateState()
	a.translate = func(ctx context.Context, s store.Settings, text string) (string, error) {
		return s.Translate(ctx, text)
	}
	marketClient := market.New(market.DefaultBaseURL)
	a.marketFetch = marketClient.List
	a.marketDocFetch = marketClient.Document
	a.openURL = mygo.Shell.OpenExternal
	a.restoreRootBrowser()
	a.pickFolder = func() (string, error) {
		a.mu.Lock()
		win := a.win
		a.mu.Unlock()
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win, Title: locales.UiFileDialogAddRootTitle(),
			ButtonLabel: locales.UiFileDialogAddRootButton(), Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
	a.openPath = mygo.Shell.OpenPath
	a.reveal = mygo.Shell.ShowItemInFolder
	a.setSource = func(mode string) { mygo.Theme.SetSource(mygo.ThemeSource(mode)) }
	// 语言：把 config.json 的 lang 解成 BCP 47 标签交给 MyGo（见
	// localeForPref：system 也由 kura 自己解，不直接把 "" 交给 MyGo）。
	a.setLocale = func(pref string) { mygo.App.SetLocale(localeForPref(pref)) }
	a.spawn = func(fn func()) { go fn() }
	// 窗口接入前的兜底：发布闭包暂存 pending，等 SetWindow 回放，
	// 保证任何 goroutine 都不会直接改 UI 状态。
	a.update = func(fn func()) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.pending = append(a.pending, fn)
	}
	return a
}

// SetWindow 把发布通道接到窗口的 Window.Update，并回放接入前积压的
// 发布闭包。
func (a *App) SetWindow(w *mygo.Window) {
	a.mu.Lock()
	a.win = w
	a.update = w.Update
	drain := a.pending
	a.pending = nil
	a.mu.Unlock()
	for _, fn := range drain {
		w.Update(fn)
	}
}

// publish 是所有后台路径唯一的发布入口：线程安全地取当前 update。
func (a *App) publish(fn func()) {
	a.mu.Lock()
	u := a.update
	a.mu.Unlock()
	u(fn)
}

// SetCheckUpdates 注入「检查 kura 应用更新」的生产实现（main.go 在窗口
// 就绪后调用：app.SetCheckUpdates(selfupdate.CheckForUpdates)）。只允许
// 生产路径接线；NewApp 与测试都不填，钩子保持 nil，按钮点下只在状态行
// 报告「更新功能未就绪」，绝不触发真实的更新检查。
func (a *App) SetCheckUpdates(fn func()) { a.checkUpdates = fn }

// SetAutoChecks 注入后台自动检查的运行时钩子（main.go 在窗口就绪后调用：
// app.SetAutoChecks(background.SetAutomaticChecks)）。偏好本身存在 config.json，
// 启动时已由 NewApp 读入 autoCheck；这里只负责把之后的切换告诉后台。与
// SetCheckUpdates 一样只允许生产路径接线：未接线时开关照常改、照常写 config.json，
// 只是后台不会收到通知。
func (a *App) SetAutoChecks(set func(on bool)) { a.autoCheckSet = set }

// SetUpdateFound 记下后台有没有查到新版本（左栏「检查更新」按钮上的小蓝点）。由
// selfupdate.Background 在后台 goroutine 里调用，所以经 a.publish 回主线程再改状态
// （同 CheckAppUpdateAsync 的范式）。它只改一个布尔值，不碰插件。
func (a *App) SetUpdateFound(available bool) {
	a.publish(func() { a.updateFound = available })
}

// ClearUpdateFound 熄灭左栏的蓝点：用户已经打开了更新窗口（点按钮，或从应用菜单选
// 「检查更新…」），不必再提醒。只在主线程调用（按钮点击与菜单回调都在主线程）。
func (a *App) ClearUpdateFound() {
	a.updateFound = false
	a.mu.Lock()
	win := a.win
	a.mu.Unlock()
	if win != nil {
		win.Invalidate()
	}
}

// SetLockPath 指定导入元数据时读取的 .skill-lock.json 路径；
// 为空时由 store 使用默认的 ~/.agents/.skill-lock.json。
func (a *App) SetLockPath(p string) { a.path = p }

// 状态筛选（Segmented 的下标）。
const (
	filterAll = iota
	filterEnabled
	filterDisabled
	filterTrashed
)

// filterState 把筛选下标换成对应的技能状态；filterAll 返回空串。
func filterState(f int) store.State {
	switch f {
	case filterEnabled:
		return store.StateEnabled
	case filterDisabled:
		return store.StateDisabled
	case filterTrashed:
		return store.StateTrashed
	}
	return ""
}

// visible 返回按状态筛选与搜索词过滤后的技能，按名字排序（不区分大小写）。
// 停用的技能与启用的混排，不沉到最后：状态只靠卡片上的开关与标签表达。
// 「全部」不含回收站：回收站里的技能只在「回收站」筛选下出现，避免与在用技能
// 混在一起。
func (a *App) visible() []*store.Skill {
	cache := &a.visibleRows
	sameSkills := cache.valid && slices.Equal(cache.skills, a.skills)
	if sameSkills && cache.query == a.query && cache.filter == a.filter {
		return cache.rows
	}
	if !sameSkills {
		cache.skills = slices.Clone(a.skills)
		cache.sorted = make([]visibleSkill, len(a.skills))
		for i, s := range a.skills {
			entry := visibleSkill{
				skill: s, name: strings.ToLower(s.Name), description: strings.ToLower(s.Description),
			}
			if s.Meta != nil {
				entry.display = strings.ToLower(s.Meta.DisplayName)
				src := s.Meta.Source
				entry.source = strings.ToLower(strings.TrimSpace(src.URL) + "\x00" + strings.TrimSpace(src.Path))
				if src.Type == store.SourceGitHub {
					entry.source += "\x00" + sourceSearchQuery(src.URL)
				}
			}
			cache.sorted[i] = entry
		}
		slices.SortStableFunc(cache.sorted, func(x, y visibleSkill) int {
			return cmp.Or(cmp.Compare(x.name, y.name), cmp.Compare(x.skill.Name, y.skill.Name))
		})
	}
	q := strings.ToLower(strings.TrimSpace(a.query))
	sourceQ := sourceSearchQuery(q)
	want := filterState(a.filter)
	var rows []*store.Skill
	for _, entry := range cache.sorted {
		s := entry.skill
		if want == "" && s.State == store.StateTrashed || want != "" && s.State != want {
			continue
		}
		if q == "" || strings.Contains(entry.name, q) ||
			strings.Contains(entry.description, q) || strings.Contains(entry.display, q) ||
			strings.Contains(entry.source, sourceQ) {
			rows = append(rows, s)
		}
	}
	cache.rows, cache.query, cache.filter, cache.valid = rows, a.query, a.filter, true
	return rows
}

// sourceSearchQuery 让 GitHub 仓库链接与 owner/repo 使用同一个搜索词；
// 名称、描述仍按原始查询匹配，不会因去掉链接外壳而扩大它们的匹配范围。
func sourceSearchQuery(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	github := false
	for _, prefix := range []string{"ssh://git@github.com/", "git@github.com:", "https://github.com/", "http://github.com/", "www.github.com/", "github.com/"} {
		if strings.HasPrefix(q, prefix) {
			q, github = strings.TrimPrefix(q, prefix), true
			break
		}
	}
	bareRepo := strings.Count(strings.TrimRight(q, "/"), "/") == 1 && !strings.HasPrefix(q, "/") && !strings.Contains(q, "://")
	if github || bareRepo {
		repo, _, _ := parseRepo(q)
		q = repo
	}
	if q == "" {
		return strings.ToLower(strings.TrimSpace(query))
	}
	return q
}

// syncSelection 在构建网格前对齐选择：网格以下标表达选择，App 以名字为准。
// 先把上一次构建里点击产生的下标换回名字，再按本次的卡片推出下标。
func (a *App) syncSelection(rows []*store.Skill) {
	if a.selIdx != a.syncIdx && a.selIdx >= 0 && a.selIdx < len(a.shown) {
		selection := a.shown[a.selIdx]
		a.setSelection(selection.name, selection.state)
	}
	// 启用、停用、移除与恢复会移动同一个技能。原状态已不存在时跟随它的新
	// 状态；筛选只隐藏行，不改变这份选择，也不让同名的另一副本抢走详情。
	if s := a.selected(); s != nil {
		a.selState = s.State
	}
	a.shown = a.shown[:0]
	a.selIdx = -1
	for i, s := range rows {
		a.shown = append(a.shown, skillSelection{name: s.Name, state: s.State})
		if a.isSelected(s) {
			a.selIdx = i
		}
	}
	a.syncIdx = a.selIdx
}

// skillKey 给控件和详情 tab 一个包含状态的稳定身份，避免同名行共用控件状态。
func skillKey(s *store.Skill) string { return string(s.State) + "\x00" + s.Name }

// selectSkill 由卡片/列表的选择动作调用，保留用户实际点中的那一个副本。
func (a *App) selectSkill(s *store.Skill) {
	a.setSelection(s.Name, s.State)
}

// selectInstalled 选择刚装进技能根目录的副本，不受仍在回收站的同名旧副本影响。
func (a *App) selectInstalled(name string) {
	a.setSelection(name, store.StateEnabled)
}

// clearSelection 关闭详情，并作废在途操作对原选择的跟随。
func (a *App) clearSelection() { a.setSelection("", "") }

// setSelection 记录明确的选择变化，包括先离开再选回同一副本的情形。
func (a *App) setSelection(name string, state store.State) {
	a.restoringRootSelection = false
	if a.selName != name || a.selState != state {
		a.selSeq++
		a.selName, a.selState = name, state
		a.pruneTranslations()
	}
}

// isSelected 只比较选择身份，不随当前的搜索词与状态筛选改变。
func (a *App) isSelected(s *store.Skill) bool {
	return s.Name == a.selName && s.State == a.selState
}

// selected 返回选中的技能（在当前目录的全部技能里找，不受筛选影响）。
func (a *App) selected() *store.Skill {
	if a.selName == "" {
		return nil
	}
	var fallback *store.Skill
	for _, s := range a.skills {
		if s.Name == a.selName {
			if s.State == a.selState {
				return s
			}
			if fallback == nil {
				fallback = s
			}
		}
	}
	return fallback
}

// busyKey 是 busy 的键：目录 + 技能名，不同目录的同名技能互不影响。
func (a *App) busyKey(name string) string { return a.cfg.Root + "\x00" + name }

// isBusy 报告当前目录里的技能 name 是否正在执行操作。
func (a *App) isBusy(name string) bool { return a.busy[a.busyKey(name)] }

// counts 统计三种状态的技能数量。
func (a *App) counts() (en, dis, trash int) {
	for _, s := range a.skills {
		switch s.State {
		case store.StateEnabled:
			en++
		case store.StateDisabled:
			dis++
		case store.StateTrashed:
			trash++
		}
	}
	return
}

// applyScan 应用一次扫描结果；失败时保留旧列表并记录错误，绝不 panic。
func (a *App) applyScan(skills []*store.Skill, err error) {
	a.skillsSeq++
	if err != nil {
		a.loadErr = err.Error()
		return
	}
	a.loadErr = ""
	if _, serr := os.Stat(a.cfg.Root); os.IsNotExist(serr) {
		a.loadErr = locales.UiStatusRootMissing(a.cfg.Root)
	}
	a.skills = skills
	a.validateRootBrowserSelection()
	a.visibleRows = visibleCache{}
	n := 0
	for _, s := range skills {
		if s.State == store.StateEnabled {
			n++
		}
	}
	a.rootCounts[a.cfg.Root] = n
	// SKILL.md 可能被更新/恢复等操作改写（磁盘上的内容已变），缓存全部
	// 失效，全文页下一帧重新读；docGen 递增让还在路上的旧读盘结果作废。
	a.skillDocs = nil
	a.docGen++
}

// publishAfterWork 在后台任务完成后重新扫描 cfg 并发布结果。key 是 busyKey；
// err 是任务本身的错误；成功时状态行显示 okMsg。期间切换了目录时只清
// busy 与报告结果，不用旧目录的扫描覆盖当前列表。
func (a *App) publishAfterWork(cfg *store.Config, gen int, key string, err error, okMsg string) {
	a.publishAfterWorkThen(cfg, gen, key, err, okMsg, nil)
}

// publishAfterWorkThen 只在操作与扫描都成功、目录仍相同时回放后续 UI 动作。
func (a *App) publishAfterWorkThen(cfg *store.Config, gen int, key string, err error, okMsg string, afterScan func()) {
	skills, scanErr := cfg.ScanAll()
	a.publish(func() {
		delete(a.busy, key)
		if gen == a.gen {
			a.applyScan(skills, scanErr)
			if err == nil && scanErr == nil && afterScan != nil {
				afterScan()
			}
		}
		if err != nil {
			a.status, a.statusErr = err.Error(), true
		} else {
			a.status, a.statusErr = okMsg, false
		}
	})
}

// ReloadAsync 在后台重新扫描当前目录，并统计其他目录的在用技能数，
// 完成后发布结果，状态行报告扫描了多少个技能。
func (a *App) ReloadAsync() { a.reload(true) }

// reload 是 ReloadAsync 的实现。announce 为 false 时，扫描成功不改状态行：
// 切换目录之后状态行留给「已添加目录」「已从列表移除」这类消息，不被
// 紧随其后的「已扫描 N 个技能」盖掉；出错时照常报告。
func (a *App) reload(announce bool) {
	if a.scanning && a.scanGen == a.gen {
		return
	}
	a.scanning = true
	a.scanGen = a.gen
	a.scanSeq++
	cfg, gen, others := a.cfg, a.gen, a.lib.Configs()
	seq, skillsSeq := a.scanSeq, a.skillsSeq
	a.spawn(func() {
		skills, err := cfg.ScanAll()
		a.publish(func() {
			if gen != a.gen || seq != a.scanSeq {
				return
			}
			a.scanning = false
			if skillsSeq != a.skillsSeq {
				// 本次刷新之后已有操作发布新列表，不能倒退到更早的快照。
				return
			}
			a.applyScan(skills, err)
			if err == nil && a.selected() == nil {
				a.clearSelection()
			}
			switch {
			case err != nil:
				a.status, a.statusErr = err.Error(), true
			case a.loadErr != "":
				a.status, a.statusErr = a.loadErr, true
			case announce:
				// 与筛选栏的「全部」、状态条的总数一致：不含回收站。
				en, dis, _ := a.counts()
				a.status, a.statusErr = locales.UiStatusScanned(en+dis), false
			}
			// 先发布当前目录，再统计其他目录。慢盘徽标不拖住列表和刷新按钮；
			// 在 UI 回调里发起第二阶段，也避免同步测试的 spawn 嵌套死锁。
			if len(others) > 1 {
				a.spawn(func() {
					counts := make(map[string]int, len(others)-1)
					for _, o := range others {
						if o.Root != cfg.Root {
							counts[o.Root] = o.CountEnabled()
						}
					}
					a.publish(func() {
						if gen != a.gen || seq != a.scanSeq {
							return
						}
						for root, n := range counts {
							// 扫描期间可能移除了其他目录，不能把徽标加回来。
							if slices.Contains(a.lib.Roots, root) {
								a.rootCounts[root] = n
							}
						}
					})
				})
			}
		})
	})
}

// ToggleAsync 切换技能的启用状态；wantOn 是切换后的目标状态。
// 实际的移动在后台执行，期间该卡片显示忙碌状态。
func (a *App) ToggleAsync(name string, wantOn bool) {
	okMsg := locales.UiStatusDisabled(name)
	from, to := store.StateEnabled, store.StateDisabled
	if wantOn {
		okMsg = locales.UiStatusEnabled(name)
		from, to = store.StateDisabled, store.StateEnabled
	}
	a.runMoveOp(name, from, to, locales.UiStatusToggling(name), okMsg, func(cfg *store.Config) error {
		var err error
		if wantOn {
			_, err = cfg.Enable(name)
		} else {
			_, err = cfg.Disable(name)
		}
		return err
	})
}

// RemoveAsync 把启用技能移入回收站（后台执行）。
func (a *App) RemoveAsync(name string) {
	a.RemoveSkillAsync(name, store.StateEnabled)
}

// RemoveSkillAsync 按发起动作的副本状态移入回收站，避免移走同名启用副本。
func (a *App) RemoveSkillAsync(name string, state store.State) {
	if state == "" {
		state = store.StateEnabled
	}
	if state != store.StateEnabled && state != store.StateDisabled {
		return
	}
	a.runMoveOp(name, state, store.StateTrashed, locales.UiStatusMovingToTrash(name), locales.UiStatusMovedToTrash(name), func(cfg *store.Config) error {
		if state == store.StateDisabled {
			_, err := cfg.RemoveDisabled(name)
			return err
		}
		_, err := cfg.Remove(name)
		return err
	})
}

// RestoreAsync 把回收站中最新一次的技能移回技能目录（后台执行）。
func (a *App) RestoreAsync(name string) {
	a.runMoveOp(name, store.StateTrashed, store.StateEnabled, locales.UiStatusRestoring(name), locales.UiStatusRestored(name), func(cfg *store.Config) error {
		_, err := cfg.Restore(name, "")
		return err
	})
}

// PurgeAsync 从回收站永久删除技能 name（后台执行；删除前 store 先写快照）。
func (a *App) PurgeAsync(name string) {
	a.runOp(name, locales.UiStatusPurging(name), locales.UiStatusPurged(name), func(cfg *store.Config) error {
		_, err := cfg.Purge(name)
		return err
	})
}

// runOp 在后台对当前目录的技能 name 执行 op：同一技能同时只跑一个操作，
// 期间显示 busy；完成后重新扫描并在状态行报告结果。
func (a *App) runOp(name, doing, okMsg string, op func(*store.Config) error) {
	a.runOpThen(name, doing, okMsg, op, nil)
}

// runMoveOp 让仍选中的原副本在移动成功后跟随目标状态。回收站可能还留有
// 同名旧记录，不能仅靠扫描后原状态是否消失推断；用户期间改选则不抢回详情。
func (a *App) runMoveOp(name string, from, to store.State, doing, okMsg string, op func(*store.Config) error) {
	selection := a.selected()
	seq := a.selSeq
	follow := selection != nil && selection.Name == name && selection.State == from
	a.runOpThen(name, doing, okMsg, op, func() {
		// 并发重扫可能先回退 selState；它不是用户改选，只以选择代数判断。
		if follow && a.selSeq == seq && a.selName == name {
			a.setSelection(name, to)
		}
	})
}

// runOpThen 保留 runOp 的忙碌与后台执行约定，额外动作只在 UI 线程执行。
func (a *App) runOpThen(name, doing, okMsg string, op func(*store.Config) error, afterScan func()) {
	key := a.busyKey(name)
	if a.busy[key] {
		return
	}
	a.busy[key] = true
	a.status, a.statusErr = doing, false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		a.publishAfterWorkThen(cfg, gen, key, op(cfg), okMsg, afterScan)
	})
}

// ImportAsync 用 .skill-lock.json 补齐缺失的 .kura-meta.json（后台执行）。
func (a *App) ImportAsync() {
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		ids, err := cfg.ImportMeta(a.path)
		okMsg := locales.UiStatusImported(len(ids))
		if err == nil && len(ids) == 0 {
			okMsg = locales.UiStatusNothingToImport()
		}
		a.publishAfterWork(cfg, gen, "", err, okMsg)
	})
}

// SelectRoot 切换到目录 root：保存旧目录的浏览状态，恢复目标目录后重新扫描。
func (a *App) SelectRoot(root string) {
	a.rootSel = root
	if root == a.cfg.Root {
		return
	}
	a.cancelCheck()
	a.saveRootBrowser()
	a.cfg = a.lib.Config(root)
	a.rootSel = a.cfg.Root
	a.gen++
	a.skills, a.loadErr = nil, ""
	a.restoreRootBrowser()
	a.pruneTranslations()
	a.visibleRows = visibleCache{}
	a.updates = map[string]bool{}
	a.review = reviewState{}
	a.status, a.statusErr = "", false
	a.reload(false)
}

// AddRootAsync 弹出文件夹选择框，把所选文件夹加入目录列表（写
// config.json）并切换过去。选择框与写盘都在后台完成。
func (a *App) AddRootAsync() {
	if a.adding {
		return
	}
	a.adding = true
	lib := a.libCopy()
	a.spawn(func() {
		dir, err := a.pickFolder()
		if err == nil && dir != "" {
			err = lib.AddRoot(dir)
		}
		a.publish(func() {
			a.adding = false
			switch {
			case dir == "" && err == nil:
				// 用户取消。
			case err != nil && !errors.Is(err, store.ErrRootExists):
				a.status, a.statusErr = err.Error(), true
			default:
				existed := err != nil
				a.lib, a.libErr = lib, ""
				a.SelectRoot(lib.Config(dir).Root)
				if !existed {
					a.status, a.statusErr = locales.UiStatusRootAdded(a.cfg.Root), false
				}
			}
		})
	})
}

// RemoveRootAsync 把目录 root 从列表移除（只改 config.json，不动磁盘上的
// 技能）。移除的是正在看的目录时回到默认目录；移除别的目录不改变当前
// 所看的目录。默认目录不能移除（store.ErrDefaultRoot 显示在状态行）。
func (a *App) RemoveRootAsync(root string) {
	lib := a.libCopy()
	a.spawn(func() {
		err := lib.RemoveRoot(root)
		a.publish(func() {
			if err != nil {
				a.status, a.statusErr = err.Error(), true
				return
			}
			a.lib = lib
			delete(a.rootCounts, root)
			// 要不要换目录在这里、UI 线程上判断：排队期间用户可能已切走。
			if root == a.cfg.Root {
				a.SelectRoot(lib.Default())
			}
			delete(a.rootBrowsers, root)
			a.status, a.statusErr = locales.UiStatusRootRemoved(root), false
		})
	})
}

// ReorderRootAsync 把目录 root 挪到列表里的第 to 个位置（后台写 config.json）。
// 默认目录固定在第一个，由 store.Library.MoveRoot 保证。
func (a *App) ReorderRootAsync(root string, to int) {
	lib := a.libCopy()
	a.spawn(func() {
		err := lib.MoveRoot(root, to)
		a.publish(func() {
			if err != nil {
				a.status, a.statusErr = err.Error(), true
				return
			}
			a.lib = lib
		})
	})
}

// DiscoverAgentRootsAsync 在后台把本机已有的 agent 技能目录加入列表（见
// store.Library.DiscoverAgentRoots，不逐个确认）。home 为空时什么也不做。
// 新加入的目录在后台统计它的在用技能数，结果回到 UI 线程后再换上列表，
// 状态行说明加入了几个；没有新目录时保持安静。
func (a *App) DiscoverAgentRootsAsync(home string) {
	if home == "" {
		return
	}
	lib := a.libCopy()
	a.spawn(func() {
		added, err := lib.DiscoverAgentRoots(home)
		counts := make(map[string]int, len(added))
		for _, r := range added {
			counts[r] = lib.Config(r).CountEnabled()
		}
		a.publish(func() {
			if err != nil {
				a.status, a.statusErr = locales.UiStatusDiscoverRootsFailed(err.Error()), true
				return
			}
			if len(added) == 0 {
				return
			}
			a.lib = lib
			for r, n := range counts {
				a.rootCounts[r] = n
			}
			a.status, a.statusErr = locales.UiStatusRootsDiscovered(len(added)), false
		})
	})
}

// libCopy 复制目录列表交给后台修改，成功后再在 UI 线程换上。带上主题、
// 排列、语言与自动检查偏好：后台的 SetTheme / SetView / SetLang / SetAutoCheck
// 都是 config.json 的「读—改—写」，副本缺了哪一项都可能把它当成空值写回去。
func (a *App) libCopy() *store.Library {
	return &store.Library{Home: a.lib.Home, Roots: append([]string(nil), a.lib.Roots...),
		Theme: a.themeMode, View: a.viewMode, Lang: a.langPref, AutoCheck: a.autoCheck}
}

// OpenPathAsync 用系统默认方式打开 path：文件夹用文件管理器，SKILL.md 用
// 默认的编辑器。打开动作在后台做，失败在状态行报告，成功不打扰。
func (a *App) OpenPathAsync(path string) {
	a.spawn(func() {
		if err := a.openPath(path); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiStatusOpenFailed(path, err.Error()), true })
		}
	})
}

// RevealAsync 在文件管理器里显示 path 并选中它（后台执行）。
func (a *App) RevealAsync(path string) {
	a.spawn(func() { a.reveal(path) })
}

// SyncTheme 启动时把外观偏好同步给 MyGo，让原生标题栏与系统菜单从一开始
// 就是对的外观。在 mygo.App.Run 之前调用（MyGo 允许这样用已存的偏好）；
// 界面本身不依赖它：皮肤由 themeMode 决定。
func (a *App) SyncTheme() { a.setSource(a.themeMode) }

// SyncLang 按语言偏好定下界面语言（见 localeForPref）。main.go 在
// mygo.App.Run 之前调用（与 SyncTheme 同型）；界面本身不依赖它——文案由
// 每帧的 locales.Xxx() 现取，切语言走 SetLangAsync。
func (a *App) SyncLang() { a.setLocale(a.langPref) }

// localeForPref 把 config.json 的 lang 取值解成交给 MyGo 的 BCP 47 标签：
// 固定语言就是 zh-Hans / en；system（含空串与不认识的值）由 kura 自己按系统
// locale 解（i18n.Resolve：主语言是 zh 的算中文，其余含空、C、POSIX 都算
// 英文），与 CLI 的 --lang 判据完全一致。
//
// 为什么 system 不直接把 "" 交给 MyGo、让它跟随用户语言：MyGo 的匹配要求
// 语言标签完全匹配，系统的 zh-TW / zh-Hant / zh-HK 匹配不上我们唯一的
// zh-Hans，于是落到 catalogs 的 source 语言（英文）。迁移前这些机器显示的
// 是中文，交给 MyGo 就是行为回退（同一个系统上 CLI 中文、窗口英文）。
//
// 代价是「跟随系统」不再在运行中实时变（用户改了系统语言要重启 kura），且
// kura 的选择会被 MyGo 存进它自己的语言文件；但每次启动都按 config.json 与
// 系统 locale 重新解一遍，所以重启后就是最新语言。
func localeForPref(pref string) string { return localeFor(pref, i18n.SystemLocale()) }

// localeFor 是 localeForPref 的实现：把系统 locale 作为参数，便于测试
// （生产传入 i18n.SystemLocale()）。
func localeFor(pref, sysLocale string) string {
	if p := store.LangPref(pref); p != i18n.LangSystem {
		return string(p)
	}
	return string(i18n.Resolve(i18n.LangSystem, sysLocale))
}

// CheckAppUpdateAsync 响应「检查更新」按钮：立刻清掉小蓝点（用户已经在看更新
// 窗口了），在状态行给一句反馈（更新检查与窗口都由 MyGo 更新插件异步完成，
// 点击后界面不能毫无反应），然后经 checkUpdates 钩子打开原生更新窗口。钩子在
// 后台调用（它立即返回、不阻塞，但走后台与其它慢调用一致）；测试注入假实现
// 计数调用。
func (a *App) CheckAppUpdateAsync() {
	// 点按钮就是「用户知道了」：蓝点立刻消失，不必等这次检查回来，也不管钩子有
	// 没有接线——用户就在更新窗口里看结果。放在最前面，重复点击也照样清。
	a.ClearUpdateFound()
	if a.checkingUpdate {
		return
	}
	if a.checkUpdates == nil {
		a.status, a.statusErr = locales.UiStatusAppUpdateNotReady(), true
		return
	}
	a.checkingUpdate = true
	a.status, a.statusErr = locales.UiStatusAppUpdateChecking(), false
	a.spawn(func() {
		a.checkUpdates()
		a.publish(func() { a.checkingUpdate = false })
	})
}

// SetAutoCheckAsync 切换「自动检查更新」为 on。界面立刻改本地值（下一帧开关就是
// 新状态），写 config.json 与通知后台在后台做：写盘失败只在状态行报告，不回滚开关，
// 与 SetThemeAsync / SetViewAsync 一样。相同的值直接返回（幂等，重复构建不会重复写
// 盘），连续切换只有最新一次会执行。
func (a *App) SetAutoCheckAsync(on bool) {
	if on == a.autoCheck {
		return
	}
	a.autoCheck = on
	seq := a.autoCheckSeq.Add(1)
	lib := a.libCopy()
	a.spawn(func() {
		a.autoCheckMu.Lock()
		defer a.autoCheckMu.Unlock()
		if a.autoCheckSeq.Load() != seq {
			return
		}
		if err := lib.SetAutoCheck(on); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiStatusAutoCheckSaveFailed(err.Error()), true })
		}
		if a.autoCheckSet != nil {
			a.autoCheckSet(on)
		}
	})
}

// SetThemeAsync 切换外观为 mode（store.ThemeSystem / ThemeLight / ThemeDark）。
// 界面立刻换皮肤——皮肤由 themeMode 决定，下一帧生效；同步原生外观与写
// config.json 在后台做，写盘失败只在状态行报告，不回滚已换的皮肤。连续
// 切换时只有最新一次会执行，避免后到的旧任务把配置写回旧值。
func (a *App) SetThemeAsync(mode string) {
	mode, err := store.NormalizeTheme(mode)
	if err != nil {
		a.status, a.statusErr = err.Error(), true
		return
	}
	if mode == a.themeMode {
		return
	}
	a.themeMode = mode
	seq := a.themeSeq.Add(1)
	lib := a.libCopy()
	a.spawn(func() {
		a.themeMu.Lock()
		defer a.themeMu.Unlock()
		if a.themeSeq.Load() != seq {
			return
		}
		a.setSource(mode)
		if err := lib.SetTheme(mode); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiStatusThemeSaveFailed(err.Error()), true })
		}
	})
}

// SetViewAsync 切换技能区的排列模式为 mode（store.ViewGrid / store.ViewList）。
// 界面立刻换排列——下一帧由 viewMode 决定；写 config.json 在后台做，写盘失败
// 只在状态行报告，不回滚已换的排列。与 SetThemeAsync 一样：相同的模式直接
// 返回（幂等，重复构建不会重复写盘），连续切换只有最新一次会执行。
func (a *App) SetViewAsync(mode string) {
	mode, err := store.NormalizeView(mode)
	if err != nil {
		a.status, a.statusErr = err.Error(), true
		return
	}
	if mode == a.viewMode {
		return
	}
	a.viewMode = mode
	seq := a.viewSeq.Add(1)
	lib := a.libCopy()
	a.spawn(func() {
		// 与外观共用同一把锁：两者都是 config.json 的「读—改—写」，串行执行。
		a.themeMu.Lock()
		defer a.themeMu.Unlock()
		if a.viewSeq.Load() != seq {
			return
		}
		if err := lib.SetView(mode); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiStatusViewSaveFailed(err.Error()), true })
		}
	})
}

// SetLangAsync 切换界面语言偏好为 pref（store.LangSystem / LangZH / LangEN）。
// 界面立刻换文案——交给 MyGo 解析（system → 用户语言），下一帧的
// locales.Xxx() 就取到新语言；菜单这类「设一次」的文字由 MyGo 自己的
// OnLocaleChanged 重新设置。写 config.json 在后台做，写盘失败只在状态行报告，
// 不回滚已换的语言（同 SetThemeAsync / SetViewAsync）。相同的偏好直接返回
// （幂等），连续切换只有最新一次写盘。
func (a *App) SetLangAsync(pref string) {
	pref, err := store.NormalizeLang(pref)
	if err != nil {
		a.status, a.statusErr = err.Error(), true
		return
	}
	if pref == a.langPref {
		return
	}
	a.langPref = pref
	a.setLocale(pref)
	seq := a.langSeq.Add(1)
	lib := a.libCopy()
	a.spawn(func() {
		// 与外观、排列共用同一把锁：三者都是 config.json 的「读—改—写」，
		// 串行执行才不会互相覆盖。
		a.themeMu.Lock()
		defer a.themeMu.Unlock()
		if a.langSeq.Load() != seq {
			return
		}
		if err := lib.SetLang(pref); err != nil {
			a.publish(func() { a.status, a.statusErr = locales.UiStatusLangSaveFailed(err.Error()), true })
		}
	})
}
