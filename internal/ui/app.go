// Package ui 实现 kura 的原生界面（基于 MyGo 的 ui 包）：
// 左侧技能列表（搜索 + 启用开关），右侧详情面板，底部计数与状态行。
//
// 线程约定：App 的字段只在 UI 线程读写；所有磁盘 I/O 都通过 spawn
// 放到后台 goroutine，结果通过 update（生产环境为 Window.Update）
// 发布回主线程。测试可以注入同步版 spawn/update。
package ui

import (
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
	cfg  *store.Config
	win  *mygo.Window
	path string // 导入元数据用的 .skill-lock.json 路径，空为默认位置

	skills []*store.Skill // 全部技能（启用 + 停用 + 回收站）
	query  string         // 搜索词
	selRow int            // 过滤后列表的选中行，-1 为未选中
	list   myui.ListState // 列表的滚动与选择状态（可持久保存）

	busy      map[string]bool // 正在执行操作的技能名
	scanning  bool            // 正在重新扫描
	loadErr   string          // 扫描/环境错误（如根目录不存在）
	status    string          // 底部状态行的文字
	statusErr bool            // 状态行是否为错误
	confirm   confirmState    // 待确认的删除操作

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
func NewApp(cfg *store.Config) *App {
	a := &App{cfg: cfg, selRow: -1, busy: map[string]bool{}}
	a.list.Selected = &a.selRow
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

// visible 返回按搜索词过滤后的技能列表，并把选中行夹紧到有效范围。
func (a *App) visible() []*store.Skill {
	q := strings.ToLower(strings.TrimSpace(a.query))
	var rows []*store.Skill
	for _, s := range a.skills {
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) ||
			(s.Meta != nil && strings.Contains(strings.ToLower(s.Meta.DisplayName), q)) {
			rows = append(rows, s)
		}
	}
	if a.selRow >= len(rows) {
		a.selRow = len(rows) - 1
	}
	return rows
}

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
		a.loadErr = "技能根目录不存在: " + a.cfg.Root
	}
	a.skills = skills
}

// publishAfterWork 在后台任务完成后重新扫描并发布结果。
// err 是任务本身的错误；成功时状态行显示 okMsg。
func (a *App) publishAfterWork(name string, err error, okMsg string) {
	skills, scanErr := a.cfg.ScanAll()
	a.publish(func() {
		delete(a.busy, name)
		a.applyScan(skills, scanErr)
		if err != nil {
			a.status, a.statusErr = err.Error(), true
		} else {
			a.status, a.statusErr = okMsg, false
		}
	})
}

// ReloadAsync 在后台重新扫描技能根目录，完成后发布结果。
func (a *App) ReloadAsync() {
	if a.scanning {
		return
	}
	a.scanning = true
	a.spawn(func() {
		skills, err := a.cfg.ScanAll()
		a.publish(func() {
			a.scanning = false
			a.applyScan(skills, err)
			a.selRow = -1
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
// 实际的移动在后台执行，期间该行显示忙碌状态。
func (a *App) ToggleAsync(name string, wantOn bool) {
	if a.busy[name] {
		return
	}
	a.busy[name] = true
	a.status, a.statusErr = "正在切换 "+name+"…", false
	a.spawn(func() {
		var err error
		okMsg := "已停用 " + name
		if wantOn {
			_, err = a.cfg.Enable(name)
			okMsg = "已启用 " + name
		} else {
			_, err = a.cfg.Disable(name)
		}
		a.publishAfterWork(name, err, okMsg)
	})
}

// RemoveAsync 把技能移入回收站（后台执行）。
func (a *App) RemoveAsync(name string) {
	if a.busy[name] {
		return
	}
	a.busy[name] = true
	a.status, a.statusErr = "正在移入回收站 "+name+"…", false
	a.spawn(func() {
		_, err := a.cfg.Remove(name)
		a.publishAfterWork(name, err, "已移入回收站 "+name)
	})
}

// RestoreAsync 把回收站中最新一次的技能移回根目录（后台执行）。
func (a *App) RestoreAsync(name string) {
	if a.busy[name] {
		return
	}
	a.busy[name] = true
	a.status, a.statusErr = "正在恢复 "+name+"…", false
	a.spawn(func() {
		_, err := a.cfg.Restore(name, "")
		a.publishAfterWork(name, err, "已恢复 "+name)
	})
}

// ImportAsync 用 .skill-lock.json 补齐缺失的 .kura-meta.json（后台执行）。
func (a *App) ImportAsync() {
	a.spawn(func() {
		ids, err := a.cfg.ImportMeta(a.path)
		okMsg := fmt.Sprintf("已导入 %d 条元数据", len(ids))
		if err == nil && len(ids) == 0 {
			okMsg = "没有可导入的元数据（lock 文件不存在或技能已有元数据）"
		}
		a.publishAfterWork("", err, okMsg)
	})
}
