package ui

import myui "github.com/egoist/mygo/ui"

// rootBrowserState 只在当前会话保留每个目录的浏览上下文。MyGo 的状态含有
// 内部行锚点、测量缓存和窗口引用，必须各自持有，不能按值复制后交叉复用。
type rootBrowserState struct {
	query     string
	filter    int
	selection skillSelection
	grid      myui.GridState
	list      myui.ListState
}

func (a *App) rootBrowser() *rootBrowserState {
	if a.rootBrowsers == nil {
		a.rootBrowsers = make(map[string]*rootBrowserState)
	}
	b := a.rootBrowsers[a.cfg.Root]
	if b == nil {
		b = &rootBrowserState{}
		// 仅当前目录的网格参与构建，因此选择下标始终绑定 App 当前视图；
		// 离开时保存技能身份，返回后再由 syncSelection 计算下标。
		b.grid.Selected = &a.selIdx
		b.grid.Key = func(i int) any {
			if i >= 0 && i < len(a.shown) {
				return a.shown[i]
			}
			return i
		}
		a.rootBrowsers[a.cfg.Root] = b
	}
	return b
}

func (a *App) saveRootBrowser() {
	b := a.rootBrowser()
	b.query, b.filter = a.query, a.filter
	b.selection = skillSelection{name: a.selName, state: a.selState}
}

func (a *App) restoreRootBrowser() {
	b := a.rootBrowser()
	a.query, a.filter = b.query, b.filter
	a.setSelection(b.selection.name, b.selection.state)
	a.restoringRootSelection = b.selection.name != ""
	// 上个目录的点击下标不可解读成新目录的选择。首次扫描的空白帧保留
	// 技能身份，扫描成功后才判断该技能是否仍然存在。
	a.selIdx, a.syncIdx, a.shown = -1, -1, nil
	a.grid, a.list = &b.grid, &b.list
}

// validateRootBrowserSelection 在成功扫描后核实恢复的副本。目录外发生的删除
// 不能把先前选中的停用副本偷偷替换成同名的启用副本；常规移动操作仍沿用
// selected 的跟随行为。主动点击或关闭详情会由 setSelection 取消这次恢复。
func (a *App) validateRootBrowserSelection() {
	if !a.restoringRootSelection {
		return
	}
	a.restoringRootSelection = false
	for _, s := range a.skills {
		if a.isSelected(s) {
			return
		}
	}
	a.clearSelection()
}
