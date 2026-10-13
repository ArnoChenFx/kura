// Package ui 实现 kura 的原生界面（基于 MyGo 的 ui 包），三栏布局：
// 左栏是技能目录列表（可添加任意文件夹），中栏是所选目录的技能卡片网格
// （搜索 + 状态筛选 + 卡片上的启用开关），右栏是所选技能的详情。
//
// 线程约定：App 的字段只在 UI 线程读写；所有磁盘 I/O 都通过 spawn
// 放到后台 goroutine，结果通过 update（生产环境为 Window.Update）
// 发布回主线程。测试可以注入同步版 spawn/update。
package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/egoist/mygo"
	myui "github.com/egoist/mygo/ui"
)

// confirmState 是“移入回收站”确认框的状态。
type confirmState struct {
	open bool
	name string
}

// App 是 kura 主窗口的状态。View 由 MyGo 在主线程反复调用，
// 从这些字段构建每一帧。
type App struct {
	lib        *store.Library // 全部技能目录
	libErr     string         // 读取/保存目录列表的错误（config.json 损坏等）
	cfg        *store.Config  // 当前选中目录的配置
	rootSel    string         // 左栏选中的目录（绑定 Sidebar），与 cfg.Root 不同时切换
	gen        int            // 目录切换代数：切换后丢弃旧目录的后台结果
	rootCounts map[string]int // 每个目录根下在用技能数（左栏徽标）
	adding     bool           // 正在选择/添加文件夹
	win        *mygo.Window
	path       string // 导入元数据用的 .skill-lock.json 路径，空为默认位置

	// pickFolder 弹出系统的文件夹选择框，返回所选路径（取消为空串）。
	// 会阻塞，只在后台 goroutine 里调用；测试里替换为假实现。
	pickFolder func() (string, error)

	skills  []*store.Skill  // 当前目录的全部技能（启用 + 停用 + 回收站）
	query   string          // 搜索词
	filter  int             // 状态筛选：filterAll/filterEnabled/filterDisabled/filterTrashed
	selName string          // 选中的技能名，空为未选中；以名字为准，跨筛选保持
	selIdx  int             // 网格的选中下标（绑定 GridState.Selected），由 selName 推出
	syncIdx int             // 上一次 syncSelection 写入 selIdx 的值：不同即网格里点了别的卡片
	shown   []string        // 上一次构建网格时各卡片的技能名，用来把点击换回名字
	grid    myui.GridState  // 卡片网格的滚动与选择状态
	updates map[string]bool // 最近一次检查发现有更新的技能（当前目录）

	busy        map[string]bool // 正在执行操作的技能（busyKey）
	checking    string          // 正在预览更新的技能名（空为无）
	checkingAll bool            // 正在全部检查
	scanning    bool            // 正在重新扫描
	loadErr     string          // 扫描/环境错误（如根目录不存在）
	status      string          // 底部状态行的文字
	statusErr   bool            // 状态行是否为错误
	confirm     confirmState    // 待确认的删除操作
	install     installState    // 安装技能弹窗
	review      reviewState     // 更新评审面板（Modal）

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
	a := &App{selIdx: -1, syncIdx: -1, busy: map[string]bool{}, rootCounts: map[string]int{}, updates: map[string]bool{}}
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		a.libErr = err.Error()
	}
	a.lib = lib
	a.cfg = cfg
	a.rootSel = lib.Default()
	a.grid.Selected = &a.selIdx
	a.grid.Key = func(i int) any {
		if i < len(a.shown) {
			return a.shown[i]
		}
		return i
	}
	a.pickFolder = func() (string, error) {
		a.mu.Lock()
		win := a.win
		a.mu.Unlock()
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win, Title: "添加技能目录", ButtonLabel: "添加", Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}
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

// visible 返回按状态筛选与搜索词过滤后的技能。「全部」不含回收站：
// 回收站里的技能只在「回收站」筛选下出现，避免与在用技能混在一起。
func (a *App) visible() []*store.Skill {
	q := strings.ToLower(strings.TrimSpace(a.query))
	want := filterState(a.filter)
	var rows []*store.Skill
	for _, s := range a.skills {
		if want == "" && s.State == store.StateTrashed || want != "" && s.State != want {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) ||
			(s.Meta != nil && strings.Contains(strings.ToLower(s.Meta.DisplayName), q)) {
			rows = append(rows, s)
		}
	}
	return rows
}

// syncSelection 在构建网格前对齐选择：网格以下标表达选择，App 以名字为准。
// 先把上一次构建里点击产生的下标换回名字，再按本次的卡片推出下标。
func (a *App) syncSelection(rows []*store.Skill) {
	if a.selIdx != a.syncIdx && a.selIdx >= 0 && a.selIdx < len(a.shown) {
		a.selName = a.shown[a.selIdx]
	}
	a.shown = a.shown[:0]
	a.selIdx = -1
	for i, s := range rows {
		a.shown = append(a.shown, s.Name)
		if s.Name == a.selName {
			a.selIdx = i
		}
	}
	a.syncIdx = a.selIdx
}

// selected 返回选中的技能（在当前目录的全部技能里找，不受筛选影响）。
func (a *App) selected() *store.Skill {
	if a.selName == "" {
		return nil
	}
	for _, s := range a.skills {
		if s.Name == a.selName {
			return s
		}
	}
	return nil
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
	if err != nil {
		a.loadErr = err.Error()
		return
	}
	a.loadErr = ""
	if _, serr := os.Stat(a.cfg.Root); os.IsNotExist(serr) {
		a.loadErr = "技能目录不存在: " + a.cfg.Root
	}
	a.skills = skills
	n := 0
	for _, s := range skills {
		if s.State == store.StateEnabled {
			n++
		}
	}
	a.rootCounts[a.cfg.Root] = n
}

// publishAfterWork 在后台任务完成后重新扫描 cfg 并发布结果。key 是 busyKey；
// err 是任务本身的错误；成功时状态行显示 okMsg。期间切换了目录时只清
// busy 与报告结果，不用旧目录的扫描覆盖当前列表。
func (a *App) publishAfterWork(cfg *store.Config, gen int, key string, err error, okMsg string) {
	skills, scanErr := cfg.ScanAll()
	a.publish(func() {
		delete(a.busy, key)
		if gen == a.gen {
			a.applyScan(skills, scanErr)
		}
		if err != nil {
			a.status, a.statusErr = err.Error(), true
		} else {
			a.status, a.statusErr = okMsg, false
		}
	})
}

// ReloadAsync 在后台重新扫描当前目录，并统计其他目录的在用技能数，
// 完成后发布结果。
func (a *App) ReloadAsync() {
	if a.scanning {
		return
	}
	a.scanning = true
	cfg, gen, others := a.cfg, a.gen, a.lib.Configs()
	a.spawn(func() {
		skills, err := cfg.ScanAll()
		counts := map[string]int{}
		for _, o := range others {
			if o.Root != cfg.Root {
				counts[o.Root] = o.CountEnabled()
			}
		}
		a.publish(func() {
			a.scanning = false
			for r, n := range counts {
				a.rootCounts[r] = n
			}
			if gen != a.gen {
				// 扫描期间切换了目录：结果作废，给新目录再扫一次。
				a.ReloadAsync()
				return
			}
			a.applyScan(skills, err)
			if a.selected() == nil {
				a.selName = ""
			}
			switch {
			case err != nil:
				a.status, a.statusErr = err.Error(), true
			case a.loadErr != "":
				a.status, a.statusErr = a.loadErr, true
			default:
				a.status, a.statusErr = fmt.Sprintf("已扫描 %d 个技能", len(skills)), false
			}
		})
	})
}

// ToggleAsync 切换技能的启用状态；wantOn 是切换后的目标状态。
// 实际的移动在后台执行，期间该卡片显示忙碌状态。
func (a *App) ToggleAsync(name string, wantOn bool) {
	okMsg := "已停用 " + name
	if wantOn {
		okMsg = "已启用 " + name
	}
	a.runOp(name, "正在切换 "+name+"…", okMsg, func(cfg *store.Config) error {
		var err error
		if wantOn {
			_, err = cfg.Enable(name)
		} else {
			_, err = cfg.Disable(name)
		}
		return err
	})
}

// RemoveAsync 把技能移入回收站（后台执行）。
func (a *App) RemoveAsync(name string) {
	a.runOp(name, "正在移入回收站 "+name+"…", "已移入回收站 "+name, func(cfg *store.Config) error {
		_, err := cfg.Remove(name)
		return err
	})
}

// RestoreAsync 把回收站中最新一次的技能移回技能目录（后台执行）。
func (a *App) RestoreAsync(name string) {
	a.runOp(name, "正在恢复 "+name+"…", "已恢复 "+name, func(cfg *store.Config) error {
		_, err := cfg.Restore(name, "")
		return err
	})
}

// runOp 在后台对当前目录的技能 name 执行 op：同一技能同时只跑一个操作，
// 期间显示 busy；完成后重新扫描并在状态行报告结果。
func (a *App) runOp(name, doing, okMsg string, op func(*store.Config) error) {
	key := a.busyKey(name)
	if a.busy[key] {
		return
	}
	a.busy[key] = true
	a.status, a.statusErr = doing, false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		a.publishAfterWork(cfg, gen, key, op(cfg), okMsg)
	})
}

// ImportAsync 用 .skill-lock.json 补齐缺失的 .kura-meta.json（后台执行）。
func (a *App) ImportAsync() {
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		ids, err := cfg.ImportMeta(a.path)
		okMsg := fmt.Sprintf("已导入 %d 条元数据", len(ids))
		if err == nil && len(ids) == 0 {
			okMsg = "没有可导入的元数据（lock 文件不存在或技能已有元数据）"
		}
		a.publishAfterWork(cfg, gen, "", err, okMsg)
	})
}

// SelectRoot 切换到目录 root：清空列表与选择，关闭评审面板，重新扫描。
func (a *App) SelectRoot(root string) {
	a.rootSel = root
	if root == a.cfg.Root {
		return
	}
	a.cfg = a.lib.Config(root)
	a.gen++
	a.skills, a.selName, a.selIdx, a.loadErr = nil, "", -1, ""
	a.updates = map[string]bool{}
	a.review = reviewState{}
	a.status, a.statusErr = "", false
	a.ReloadAsync()
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
					a.status, a.statusErr = "已添加目录 "+a.cfg.Root, false
				}
			}
		})
	})
}

// RemoveRootAsync 把当前目录从列表移除（只改 config.json，不动磁盘上的
// 技能），然后回到默认目录。
func (a *App) RemoveRootAsync() {
	root := a.cfg.Root
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
			a.SelectRoot(lib.Default())
			a.status, a.statusErr = "已从列表移除 "+root+"（文件夹未改动）", false
		})
	})
}

// libCopy 复制目录列表交给后台修改，成功后再在 UI 线程换上。
func (a *App) libCopy() *store.Library {
	return &store.Library{Home: a.lib.Home, Roots: append([]string(nil), a.lib.Roots...)}
}
