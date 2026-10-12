package ui

import (
	"fmt"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// stateText 把技能状态翻译成短标签。
func stateText(s store.State) string {
	switch s {
	case store.StateDisabled:
		return "已停用"
	case store.StateTrashed:
		return "回收站"
	default:
		return "已启用"
	}
}

// stateColor 返回状态标签的颜色。
func stateColor(c *myui.Context, s store.State) myui.Color {
	t := c.Theme()
	switch s {
	case store.StateDisabled:
		return t.Warning
	case store.StateTrashed:
		return t.Danger
	default:
		return t.Success
	}
}

// sourceText 概括技能的来源类型。
func sourceText(s *store.Skill) string {
	if s.Meta == nil {
		return "无元数据"
	}
	switch s.Meta.Source.Type {
	case store.SourceGitHub:
		return "GitHub"
	case store.SourceLocal:
		return "本地"
	case "":
		return "未知来源"
	default:
		return s.Meta.Source.Type
	}
}

// metaLine 是详情面板里的一行「标签：值」。wrap 为 true 时值允许换行。
func metaLine(c *myui.Context, k, v string, wrap bool) {
	myui.Row(c).Gap(8).AlignItems(myui.Start).Children(func() {
		myui.Text(c, k).Width(90).TextColor(c.Theme().TextMuted)
		value := myui.Text(c, v)
		if !wrap {
			value = value.SingleLine()
		}
	})
}

// View 构建 kura 主窗口的一帧，结构：
//
//	┌──────────────┬──────────────────────┐
//	│ 搜索/刷新/导入 │ 选中技能的详情        │
//	├──────────────┤ （.kura-meta.json）   │
//	│ 技能列表     │                       │
//	├──────────────┴──────────────────────┤
//	│ 状态/错误行 · 计数 · 根目录          │
//	└─────────────────────────────────────┘
func (a *App) View(c *myui.Context) {
	// 首帧触发一次后台初始扫描（磁盘 I/O 不在 UI 线程做）。
	if !a.loaded {
		a.loaded = true
		a.ReloadAsync()
	}
	rows := a.visible()

	myui.Column(c).Fill().Children(func() {
		a.viewToolbar(c)
		a.viewBody(c, rows)
		a.viewFooter(c)
	})

	// 「移入回收站」确认框；最后一个按钮是默认按钮（回车触发），
	// Escape 或点按钮后 *open 置 false。确认按钮的文字要与详情面板的
	// 「移入回收站」按钮区分开，便于测试与辨识。
	if n := myui.AlertDialog(c, &a.confirm.open, "移入回收站？",
		"技能 "+a.confirm.name+" 将从根目录移入 kura 回收站，可随时恢复；移动前会自动写入内容快照。",
		"取消", "确认移入"); n == 1 {
		a.RemoveAsync(a.confirm.name)
	}

	// 更新评审面板（检查更新/全部检查的结果 + 应用/取消）。
	a.viewReviewModal(c)
}

// viewToolbar 是顶栏：搜索框、刷新、全部检查、导入元数据。
func (a *App) viewToolbar(c *myui.Context) {
	myui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(myui.Center).Children(func() {
		myui.SearchField(c, &a.query).Grow(1).Label("搜索技能")
		if a.scanning || a.checkingAll || a.checking != "" {
			myui.Spinner(c)
		}
		if myui.Button(c, "刷新").Clicked() {
			a.ReloadAsync()
		}
		// 全部检查：逐个预览根目录技能的更新（评审面板见 viewReviewModal）。
		if myui.Button(c, "全部检查").Disabled(a.checkingAll || a.checking != "").Clicked() {
			a.CheckAllAsync()
		}
		if myui.Button(c, "导入元数据").Clicked() {
			a.ImportAsync()
		}
	})
}

// viewBody 是中间部分：左侧技能列表 + 分隔线 + 右侧详情。
func (a *App) viewBody(c *myui.Context, rows []*store.Skill) {
	myui.Row(c).Fill().AlignItems(myui.Stretch).Children(func() {
		a.viewList(c, rows)
		myui.Divider(c)
		a.viewDetail(c, rows)
	})
}

// viewList 是左侧技能列表。目录缺失或为空时显示提示而不是崩溃。
func (a *App) viewList(c *myui.Context, rows []*store.Skill) {
	t := c.Theme()
	myui.Column(c).Width(290).FillHeight().Padding(8, 12).Gap(6).Children(func() {
		if a.loadErr != "" {
			myui.Text(c, a.loadErr).TextColor(t.Danger)
		}
		if len(rows) == 0 {
			msg := "没有技能"
			if a.query != "" {
				msg = "没有匹配「" + a.query + "」的技能"
			}
			myui.Box(c).Grow(1).Center().Children(func() {
				myui.Text(c, msg).TextColor(t.TextMuted)
			})
			return
		}
		// 虚拟列表：只构建可见行；ItemKey 让行状态跟随技能名。
		myui.List(c, &a.list, len(rows)).Grow(1).
			ItemKey(func(i int) any { return rows[i].Name }).
			Rows(func(r myui.ListRow) {
				a.viewSkillRow(r, rows[r.Index])
			})
	})
}

// viewSkillRow 是列表的一行：名称/状态/来源 + 启用开关或恢复按钮。
// 行的选中态与悬停态由 List 自身绘制。
func (a *App) viewSkillRow(r myui.ListRow, s *store.Skill) {
	c := r.Context
	t := c.Theme()
	busy := a.busy[s.Name]

	myui.Row(c).FillWidth().Padding(6, 8).Gap(8).AlignItems(myui.Center).Children(func() {
		myui.Column(c).Grow(1).Gap(2).Children(func() {
			myui.Text(c, s.Name).SingleLine().Bold()
			myui.Row(c).Gap(6).AlignItems(myui.Center).Children(func() {
				myui.Text(c, stateText(s.State)).SingleLine().
					FontSize(t.FontSize - 2).TextColor(stateColor(c, s.State))
				myui.Text(c, "·").FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				myui.Text(c, sourceText(s)).SingleLine().FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				if s.MetaErr != "" {
					myui.Text(c, "元数据损坏").SingleLine().FontSize(t.FontSize - 2).TextColor(t.Danger)
				}
				if busy {
					myui.Spinner(c)
				}
			})
		})

		// 回收站里的技能只能恢复；其余显示启用开关（开 = 在根目录）。
		if s.State == store.StateTrashed {
			if busy {
				myui.Spinner(c)
			} else if myui.Button(c, "恢复").Clicked() {
				a.RestoreAsync(s.Name)
			}
			return
		}
		// Switch 的输入在构建时应用：Changed 返回 true 的这次构建里，
		// on 已是切换后的目标状态（文档允许绑定局部值并在同一构建处理）。
		on := s.State == store.StateEnabled
		sw := myui.Switch(c, &on).Disabled(busy).Label(s.Name + " 开关")
		if sw.Changed() {
			a.ToggleAsync(s.Name, on)
		}
	})
}

// viewDetail 是右侧详情面板：选中技能的元数据字段与操作按钮。
func (a *App) viewDetail(c *myui.Context, rows []*store.Skill) {
	t := c.Theme()
	myui.Column(c).Grow(1).FillHeight().Padding(12, 16).Gap(10).Children(func() {
		if a.selRow < 0 || a.selRow >= len(rows) {
			myui.Box(c).Grow(1).Center().Children(func() {
				myui.Text(c, "选择左侧的技能查看详情").TextColor(t.TextMuted)
			})
			return
		}
		s := rows[a.selRow]
		busy := a.busy[s.Name]

		myui.Text(c, s.Name).Bold().FontSize(t.FontSize + 4)
		myui.Row(c).Gap(8).AlignItems(myui.Center).Children(func() {
			myui.Text(c, stateText(s.State)).SingleLine().TextColor(stateColor(c, s.State))
			myui.Text(c, "·").TextColor(t.TextMuted)
			myui.Text(c, sourceText(s)).SingleLine().TextColor(t.TextMuted)
			if !s.HasSKILLMD && s.State != store.StateTrashed {
				myui.Text(c, "·").TextColor(t.TextMuted)
				myui.Text(c, "缺少 SKILL.md").SingleLine().TextColor(t.Warning)
			}
		})

		myui.Divider(c)

		// 元数据字段；缺失或损坏时给出原因，绝不 panic。
		if m := s.Meta; m != nil {
			metaLine(c, "来源", sourceURLText(m), true)
			if m.Source.Type == store.SourceGitHub {
				if m.Source.Ref != "" {
					metaLine(c, "Ref", m.Source.Ref, false)
				}
				if m.Source.SubPath != "" {
					metaLine(c, "SubPath", m.Source.SubPath, false)
				}
				if m.Source.Commit != "" {
					metaLine(c, "Commit", m.Source.Commit, false)
				}
			}
			if m.PluginName != "" {
				metaLine(c, "Plugin", m.PluginName, false)
			}
			metaLine(c, "安装时间", m.InstalledAt, false)
			metaLine(c, "更新时间", m.UpdatedAt, false)
			metaLine(c, "内容哈希", shortHash(m.ContentHash), false)
		} else if s.MetaErr != "" {
			myui.Text(c, "元数据读取失败："+s.MetaErr).TextColor(t.Danger)
		} else {
			myui.Text(c, "没有 .kura-meta.json（非 kura 安装，或尚未导入元数据）").TextColor(t.TextMuted)
		}
		metaLine(c, "所在位置", s.Dir, true)

		// 操作区：按状态给出可用动作，后台执行期间显示忙碌。
		myui.Row(c).Gap(8).MarginY(4).AlignItems(myui.Center).Children(func() {
			if busy {
				myui.Spinner(c)
				return
			}
			switch s.State {
			case store.StateEnabled:
				// 检查更新：预览上游差异，弹出评审面板（不直接改盘）。
				if myui.Button(c, "检查更新").Label("详情:检查更新").Clicked() {
					a.CheckAsync(s.Name)
				}
				if myui.Button(c, "停用").Clicked() {
					a.ToggleAsync(s.Name, false)
				}
				if myui.Button(c, "移入回收站").Label("详情:移入回收站").Clicked() {
					a.confirm = confirmState{open: true, name: s.Name}
				}
			case store.StateDisabled:
				if myui.Button(c, "启用").Clicked() {
					a.ToggleAsync(s.Name, true)
				}
				if myui.Button(c, "移入回收站").Label("详情:移入回收站").Clicked() {
					a.confirm = confirmState{open: true, name: s.Name}
				}
			case store.StateTrashed:
				if myui.Button(c, "恢复到根目录").Clicked() {
					a.RestoreAsync(s.Name)
				}
			}
		})
	})
}

// viewFooter 是底部状态行：最近一次操作的结果 + 计数 + 根目录路径。
func (a *App) viewFooter(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	myui.Row(c).FillWidth().Padding(6, 12).Gap(10).AlignItems(myui.Center).Children(func() {
		if a.status != "" {
			color := t.Success
			if a.statusErr {
				color = t.Danger
			}
			myui.Text(c, a.status).SingleLine().TextColor(color)
			myui.Text(c, "·").TextColor(t.TextMuted)
		}
		myui.Text(c, fmt.Sprintf("启用 %d · 停用 %d · 回收站 %d", en, dis, trash)).
			SingleLine().TextColor(t.TextMuted)
		myui.Spacer(c)
		myui.Text(c, "根目录："+a.cfg.Root).SingleLine().TextColor(t.TextMuted)
	})
}

// sourceURLText 概括元数据里的来源信息。
func sourceURLText(m *store.Meta) string {
	switch m.Source.Type {
	case store.SourceGitHub:
		return m.Source.URL
	case store.SourceLocal:
		return m.Source.Path
	default:
		if m.Source.Type == "" {
			return "未知"
		}
		return m.Source.Type
	}
}

// shortHash 缩短内容哈希便于展示。
func shortHash(h string) string {
	if h == "" {
		return "—"
	}
	if i := strings.Index(h, ":"); i >= 0 && len(h) > i+13 {
		return h[:i+13] + "…"
	}
	return h
}

// diffStatusText 把文件差异状态翻译成短标签。
func diffStatusText(status string) string {
	switch status {
	case store.DiffAdded:
		return "新增"
	case store.DiffRemoved:
		return "删除"
	default:
		return "修改"
	}
}

// diffStatusColor 返回差异标签的颜色：新增绿、删除红、修改琥珀。
func diffStatusColor(c *myui.Context, status string) myui.Color {
	t := c.Theme()
	switch status {
	case store.DiffAdded:
		return t.Success
	case store.DiffRemoved:
		return t.Danger
	default:
		return t.Warning
	}
}

// viewReviewModal 渲染更新评审面板（Modal）：
//
//   - 单技能模式：标题为「更新 <skill>」；副行显示来源类型/上游 commit 与
//     目录级新旧哈希；正文逐行列出每个变更文件（状态、路径、±行数、新哈希）；
//     无变化时提示「与上游一致」；底部 应用更新 / 取消。
//   - 全部检查模式：标题「全部检查」；正文每个技能一小节，有变化的列出
//     文件摘要行，无变化的显示「已是最新」，失败的显示错误；底部
//     「应用全部更新」（逐个 Update 有变化的技能）/ 关闭。
//
// Apply 走 ApplyAsync（后台执行，完成后重扫并关闭面板）；取消仅关面板，
// 不动磁盘。Escape/点击外部关闭由 Modal 自带，语义与取消相同。
func (a *App) viewReviewModal(c *myui.Context) {
	r := &a.review
	t := c.Theme()
	myui.Modal(c, &r.open, func() {
		myui.Column(c).Width(560).Gap(12).Children(func() {
			switch {
			case r.all:
				myui.Text(c, "全部检查").FontSize(16).Bold()
				if len(r.entries) == 0 {
					myui.Text(c, "根目录里没有可检查的技能").TextColor(t.TextMuted)
				}
				myui.Scroll(c).Grow(1).Height(340).Children(func() {
					myui.Column(c).FillWidth().Gap(10).Children(func() {
						for _, e := range r.entries {
							a.viewReviewEntry(c, e)
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, "关闭").Clicked() {
						a.CloseReview()
					}
					label := "应用全部更新"
					if r.applyBusy {
						label = "正在应用…"
					}
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || !reviewHasChanges(r.entries)).Clicked() {
						a.ApplyAsync()
					}
				})
			case r.preview != nil:
				p := r.preview
				myui.Text(c, "更新 "+r.skill).FontSize(16).Bold()
				myui.Row(c).Gap(8).AlignItems(myui.Center).Children(func() {
					myui.Text(c, previewMetaText(p)).SingleLine().TextColor(t.TextMuted)
				})
				if !p.HasChanges() {
					myui.Text(c, "与上游一致，无需更新").TextColor(t.Success)
				}
				myui.Scroll(c).Grow(1).Height(320).Children(func() {
					myui.Column(c).FillWidth().Gap(2).Children(func() {
						for _, f := range p.Files {
							a.viewDiffRow(c, f)
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, "取消").Clicked() {
						a.CloseReview()
					}
					label := "应用更新"
					if r.applyBusy {
						label = "正在应用…"
					}
					// 无变化时禁用应用（ApplyAsync 对空名单也会直接返回）。
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || !p.HasChanges()).Clicked() {
						a.ApplyAsync()
					}
				})
			default:
				// 理论不可达：open 但无数据。给占位并允许关闭，绝不 panic。
				myui.Text(c, "没有可显示的检查结果").TextColor(t.TextMuted)
				if myui.Button(c, "关闭").Clicked() {
					a.CloseReview()
				}
			}
		})
	})
}

// viewReviewEntry 渲染全部检查汇总里的一个技能小节。
func (a *App) viewReviewEntry(c *myui.Context, e skillReviewEntry) {
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(4).Children(func() {
		myui.Row(c).FillWidth().Gap(8).AlignItems(myui.Center).Children(func() {
			myui.Text(c, e.skill).Bold()
			switch {
			case e.err != "":
				myui.Text(c, "检查失败").SingleLine().TextColor(t.Danger)
			case !e.preview.HasChanges():
				myui.Text(c, "已是最新").SingleLine().TextColor(t.Success)
			default:
				added, changed, removed := e.preview.Counts()
				myui.Text(c, fmt.Sprintf("+%d ~%d -%d（%d 个文件）", added, changed, removed, len(e.preview.Files))).
					SingleLine().TextColor(t.Warning)
			}
		})
		// 错误详情或变更文件摘要（每个文件一行）。
		if e.err != "" {
			myui.Text(c, e.err).TextColor(t.Danger)
			return
		}
		for _, f := range e.preview.Files {
			a.viewDiffRow(c, f)
		}
	})
}

// viewDiffRow 渲染一个变更文件行：状态标签 + 路径 + ±行数 + 新内容哈希。
func (a *App) viewDiffRow(c *myui.Context, f store.FileDiff) {
	t := c.Theme()
	myui.Row(c).FillWidth().Padding(2, 8).Gap(8).AlignItems(myui.Center).Children(func() {
		myui.Text(c, diffStatusText(f.Status)).SingleLine().Width(44).
			TextColor(diffStatusColor(c, f.Status))
		myui.Text(c, f.Path).SingleLine().Grow(1)
		counts := fmt.Sprintf("+%d/-%d", f.Added, f.Removed)
		myui.Text(c, counts).SingleLine().TextColor(t.TextMuted)
		if f.NewHash != "" {
			myui.Text(c, f.NewHash[:8]).SingleLine().TextColor(t.TextMuted)
		}
	})
}

// previewMetaText 生成单技能评审面板的元信息行。
func previewMetaText(p *store.UpdatePreview) string {
	parts := []string{"来源: " + p.SourceType}
	if p.Commit != "" {
		parts = append(parts, "上游 commit: "+shortCommit(p.Commit))
	}
	parts = append(parts, "旧 "+shortHash(p.OldHash), "新 "+shortHash(p.NewHash))
	return strings.Join(parts, " · ")
}

// shortCommit 缩短 commit 哈希用于展示。
func shortCommit(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// reviewHasChanges 报告全部检查汇总里是否存在任何可应用的变更。
func reviewHasChanges(entries []skillReviewEntry) bool {
	for _, e := range entries {
		if e.preview != nil && e.preview.HasChanges() {
			return true
		}
	}
	return false
}
