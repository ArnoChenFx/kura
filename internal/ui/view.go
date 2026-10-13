package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// 三栏的固定宽度：左栏放目录名与徽标，右栏放详情字段；中栏占满剩余。
const (
	rootsWidth  = 216
	detailWidth = 320
	// cardMinWidth / cardHeight 是卡片的最小宽度与固定高度：网格按窗口宽度
	// 决定列数，列宽在最小宽度之上平分剩余空间。
	cardMinWidth = 232
	cardHeight   = 128
)

// View 构建 kura 主窗口的一帧，三栏结构：
//
//	┌──────────┬──────────────────────────────┬────────────┐
//	│ kura     │ 目录名 · 路径    刷新 检查 导入 │ 详情（选中   │
//	│ 技能目录  │ 🔍 搜索…   [全部|已启用|停用|回收站] │ 卡片后出现） │
//	│ ▸skills 9│ ┌──────┐ ┌──────┐ ┌──────┐     │ 名字 状态    │
//	│  other  3│ │卡片 ⬜│ │卡片 ⬜│ │卡片 ⬜│     │ 描述         │
//	│          │ └──────┘ └──────┘ └──────┘     │ 元数据       │
//	│ +添加文件夹│ 状态条                         │ 操作         │
//	└──────────┴──────────────────────────────┴────────────┘
func (a *App) View(c *myui.Context) {
	// 首帧触发一次后台初始扫描（磁盘 I/O 不在 UI 线程做）。
	if !a.loaded {
		a.loaded = true
		a.ReloadAsync()
	}
	// 左栏的选择在上一次构建的输入里改了 rootSel：在这里切换目录。
	// 以状态比较为守卫，重复构建不会重复切换。
	if a.rootSel != a.cfg.Root {
		a.SelectRoot(a.rootSel)
	}
	rows := a.visible()
	a.syncSelection(rows)
	sel := a.selected()

	myui.Row(c).Fill().AlignItems(myui.Stretch).Children(func() {
		a.viewRoots(c)
		myui.Divider(c)
		myui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			a.viewMainHeader(c)
			a.viewFilterBar(c)
			a.viewErrorBanner(c)
			a.viewGrid(c, rows)
			a.viewStatusBar(c)
		})
		if sel != nil {
			myui.Divider(c)
			a.viewDetail(c, sel)
		}
	})

	// 「移入回收站」确认框；最后一个按钮是默认按钮（回车触发），
	// Escape 或点按钮后 *open 置 false。
	if n := myui.AlertDialog(c, &a.confirm.open, "移入回收站？",
		"技能 "+a.confirm.name+" 将从技能目录移入 kura 回收站，可随时恢复；移动前会自动写入内容快照。",
		"取消", "确认移入"); n == 1 {
		a.RemoveAsync(a.confirm.name)
	}

	// 更新评审面板（检查更新/全部检查的结果 + 应用/取消）。
	a.viewReviewModal(c)
}

// viewRoots 是左栏：应用名、技能目录列表（每项带在用技能数）与底部的
// 「添加文件夹」。默认目录总在第一个。
func (a *App) viewRoots(c *myui.Context) {
	t := c.Theme()
	myui.Column(c).Width(rootsWidth).Shrink(0).FillHeight().Children(func() {
		myui.Row(c).Padding(t.Space(3), t.Space(4), t.Space(2), t.Space(4)).
			AlignItems(myui.Center).Gap(t.Space(2)).Children(func() {
			myui.Text(c, "kura").Bold().FontSize(t.FontSize + 4)
		})
		myui.Sidebar(c, &a.rootSel, func() {
			myui.SidebarSection(c, "技能目录", nil, func() {
				for i, root := range a.lib.Roots {
					icon := iconFolder
					if i == 0 {
						icon = iconHome
					}
					item := myui.SidebarItem(c, root, icon, rootLabel(root)).Tooltip(root)
					if n, ok := a.rootCounts[root]; ok {
						item.Children(func() { myui.Badge(c, itoa(n)) })
					}
				}
			})
		}).Grow(1).FillWidth()
		if a.libErr != "" {
			myui.Text(c, "目录列表读取失败："+a.libErr).MaxLines(3).
				FontSize(t.FontSize-2).TextColor(t.Danger).Padding(0, t.Space(4))
		}
		myui.Box(c).Padding(t.Space(2), t.Space(3), t.Space(3), t.Space(3)).Children(func() {
			ab := myui.Button(c, "").Label("添加文件夹").FillWidth().Disabled(a.adding)
			ab.Children(func() {
				myui.Icon(c, iconPlus)
				myui.Text(c, "添加文件夹")
			})
			if ab.Clicked() {
				a.AddRootAsync()
			}
		})
	})
}

// rootLabel 是目录在左栏里的名字：最后两级路径（如 .agents/skills），
// 比单独的 skills 更好分辨。
func rootLabel(root string) string {
	parent, base := filepath.Split(filepath.Clean(root))
	parent = filepath.Base(filepath.Clean(parent))
	if parent == "" || parent == "." || parent == string(filepath.Separator) || base == "" {
		return root
	}
	return parent + "/" + base
}

// viewMainHeader 是中栏头部：当前目录名与完整路径，右侧是作用于整个目录
// 的图标按钮（刷新、全部检查、导入元数据、从列表移除）。
func (a *App) viewMainHeader(c *myui.Context) {
	t := c.Theme()
	isDefault := a.cfg.Root == a.lib.Default()
	myui.Row(c).FillWidth().Padding(t.Space(3), t.Space(4), t.Space(1), t.Space(4)).
		Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
			myui.Text(c, rootLabel(a.cfg.Root)).Bold().FontSize(t.FontSize + 3).SingleLine()
			myui.Text(c, a.cfg.Root).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
		if a.scanning || a.checkingAll || a.checking != "" {
			myui.Spinner(c)
		}
		iconButton(c, iconRefresh, "刷新", a.scanning, a.ReloadAsync)
		iconButton(c, iconCheckAll, "全部检查", a.checkingAll || a.checking != "", a.CheckAllAsync)
		// .skill-lock.json 只描述默认目录里的技能。
		if isDefault {
			iconButton(c, iconImport, "导入元数据", false, a.ImportAsync)
		} else {
			iconButton(c, iconUnlink, "从列表移除此目录", false, a.RemoveRootAsync)
		}
	})
}

// viewFilterBar 是搜索框 + 状态筛选（带数量）。
func (a *App) viewFilterBar(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	// 窄的时候筛选换到搜索框下一行（Wrap），而不是互相挤压。
	myui.Row(c).FillWidth().Wrap().Padding(t.Space(2), t.Space(4)).Gap(t.Space(2)).
		AlignItems(myui.Center).Children(func() {
		searchBox(c, &a.query).Grow(1).Basis(t.Space(50)).MaxWidth(t.Space(80))
		myui.Segmented(c, &a.filter,
			"全部 "+itoa(en+dis), "启用 "+itoa(en), "停用 "+itoa(dis), "回收站 "+itoa(trash)).
			Shrink(0)
	})
}

// viewErrorBanner 是全局错误横幅：目录缺失或扫描失败时出现在卡片上方。
func (a *App) viewErrorBanner(c *myui.Context) {
	if a.loadErr == "" {
		return
	}
	t := c.Theme()
	myui.Box(c).FillWidth().Margin(t.Space(1), t.Space(4), 0, t.Space(4)).
		Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
		Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.4)).
		Children(func() {
			myui.Text(c, a.loadErr).SingleLine().TextColor(t.Danger)
		})
}

// viewGrid 是技能卡片网格：按宽度自动换列，只构建可见的行。
func (a *App) viewGrid(c *myui.Context, rows []*store.Skill) {
	t := c.Theme()
	if len(rows) == 0 {
		a.viewGridEmpty(c)
		return
	}
	myui.GridView(c, &a.grid, len(rows), cardMinWidth, cardHeight, func(i int) {
		a.viewCard(c, rows[i], i == a.selIdx)
	}).Grow(1).FillWidth().Padding(t.Space(1), t.Space(3))
}

// viewGridEmpty 是没有卡片时的占位：区分「搜索无结果」「筛选下为空」与
// 「目录里没有技能」。
func (a *App) viewGridEmpty(c *myui.Context) {
	t := c.Theme()
	msg, hint := "这个目录里还没有技能", "把技能文件夹放进该目录，或用命令行 kura add 安装"
	switch {
	case a.query != "":
		msg, hint = "没有匹配「"+a.query+"」的技能", "换个关键词试试"
	case a.filter != filterAll:
		msg, hint = "没有"+stateText(filterState(a.filter))+"的技能", ""
	}
	myui.Box(c).Grow(1).FillWidth().Center().Children(func() {
		myui.Column(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Icon(c, iconInbox).TextColor(t.TextMuted).FontSize(t.FontSize * 3)
			myui.Text(c, msg).Bold()
			if hint != "" {
				myui.Text(c, hint).TextColor(t.TextMuted)
			}
		})
	})
}

// viewCard 是一张技能卡片：
//
//	[名字 …………………… 开关]
//	[描述，最多两行          ]
//	[来源 · 提示      有更新]
//
// 停用的卡片名字变灰，回收站卡片加删除线并把开关换成「恢复」。
func (a *App) viewCard(c *myui.Context, s *store.Skill, selected bool) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	border := t.Border
	if selected {
		border = t.Accent
	}
	myui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(1.5)).
		Radius(t.Radius+2).Background(t.Surface).Border(1, border).Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			name := myui.Text(c, s.Name).SingleLine().FontWeight(600).Grow(1).MinWidth(0)
			if s.State != store.StateEnabled {
				name.TextColor(t.TextMuted)
			}
			if s.State == store.StateTrashed {
				name.Strikethrough()
			}
			switch {
			case busy:
				myui.Spinner(c)
			case s.State == store.StateTrashed:
				rb := myui.Button(c, "").Label("恢复").Tooltip("恢复到技能目录")
				rb.Children(func() { myui.Icon(c, iconRestore) })
				if rb.Clicked() {
					a.RestoreAsync(s.Name)
				}
			default:
				// Switch 的输入在构建时应用：Changed 返回 true 的这次构建里，
				// on 已是切换后的目标状态。
				on := s.State == store.StateEnabled
				sw := myui.Switch(c, &on).Label(s.Name + " 开关")
				if sw.Changed() {
					a.ToggleAsync(s.Name, on)
				}
			}
		})
		desc, color := s.Description, t.TextMuted
		if desc == "" {
			desc = "（SKILL.md 没有 description）"
			color = color.Alpha(0.6)
		}
		myui.Text(c, desc).MaxLines(2).FontSize(t.FontSize - 1).TextColor(color).Grow(1)
		myui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Icon(c, sourceIcon(s)).TextColor(t.TextMuted).FontSize(t.FontSize - 1)
			myui.Text(c, rowCaption(s)).SingleLine().Grow(1).MinWidth(0).
				FontSize(t.FontSize - 2).TextColor(captionColor(c, s))
			if a.updates[s.Name] {
				pill(c, "有更新", t.Accent)
			} else if s.State == store.StateDisabled {
				pill(c, "已停用", t.Warning)
			}
		})
	})
}

// viewDetail 是右栏：所选技能的详情，按「标题 → 描述 → 元数据 → 位置 →
// 操作」分组；右上角的关闭按钮取消选择、收起右栏。
func (a *App) viewDetail(c *myui.Context, s *store.Skill) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	myui.Column(c).Width(detailWidth).Shrink(0).FillHeight().Children(func() {
		myui.Scroll(c).Grow(1).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(t.Space(3), t.Space(4)).Gap(t.Space(3)).Children(func() {
				// 标题区：名字 + 关闭；状态徽标 + 来源。
				myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Start).Children(func() {
					name := myui.Text(c, s.Name).Bold().FontSize(t.FontSize + 4).Grow(1).MinWidth(0)
					if s.State == store.StateTrashed {
						name.Strikethrough()
					}
					iconButton(c, iconClose, "关闭详情", false, func() { a.selName = "" })
				})
				myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
					stateBadge(c, s.State)
					myui.Text(c, sourceText(s)).SingleLine().TextColor(t.TextMuted)
				})
				if s.Description != "" {
					myui.Text(c, s.Description)
				}
				if !s.HasSKILLMD && s.State != store.StateTrashed {
					myui.Text(c, "缺少 SKILL.md").TextColor(t.Warning)
				}

				myui.Divider(c)
				sectionHeader(c, "元数据")
				a.viewDetailMeta(c, s)

				sectionHeader(c, "位置")
				myui.Text(c, s.Dir).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
		})
		myui.Divider(c)
		myui.Box(c).FillWidth().Padding(t.Space(3), t.Space(4)).Children(func() {
			a.viewDetailActions(c, s, busy)
		})
	})
}

// viewDetailMeta 是元数据字段；缺失或损坏时给出原因，绝不 panic。
func (a *App) viewDetailMeta(c *myui.Context, s *store.Skill) {
	t := c.Theme()
	m := s.Meta
	switch {
	case m != nil:
	case s.MetaErr != "":
		myui.Text(c, "元数据读取失败："+s.MetaErr).TextColor(t.Danger)
		return
	default:
		myui.Text(c, "没有 .kura-meta.json（非 kura 安装，或尚未导入元数据）").TextColor(t.TextMuted)
		return
	}
	myui.Form(c, func() {
		metaField(c, "来源", sourceURLText(m), true)
		if m.Source.Type == store.SourceGitHub {
			if m.Source.Ref != "" {
				metaField(c, "Ref", m.Source.Ref, false)
			}
			if m.Source.SubPath != "" {
				metaField(c, "SubPath", m.Source.SubPath, true)
			}
			if m.Source.Commit != "" {
				metaField(c, "Commit", shortCommit(m.Source.Commit), false)
			}
		}
		if m.PluginName != "" {
			metaField(c, "Plugin", m.PluginName, false)
		}
		if m.InstalledAt != "" {
			metaField(c, "安装", shortTime(m.InstalledAt), false)
		}
		if m.UpdatedAt != "" {
			metaField(c, "更新", shortTime(m.UpdatedAt), false)
		}
		metaField(c, "哈希", shortHash(m.ContentHash), false)
	})
}

// viewDetailActions 是详情底部的操作行，按技能状态分组：
// 启用 → 检查更新（主）+ 停用 + 移入回收站（图标）；
// 停用 → 启用（主）+ 移入回收站（图标）；
// 回收站 → 恢复到技能目录（主）。
func (a *App) viewDetailActions(c *myui.Context, s *store.Skill, busy bool) {
	t := c.Theme()
	if busy {
		myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, "正在操作 "+s.Name+"…").SingleLine().TextColor(t.TextMuted)
		})
		return
	}
	// trashButton 是「移入回收站」：Label 保持「详情:移入回收站」供测试与无障碍。
	trashButton := func() {
		myui.Spacer(c)
		b := myui.Button(c, "").Label("详情:移入回收站").Tooltip("移入回收站")
		b.Children(func() { myui.Icon(c, iconTrash).TextColor(t.Danger) })
		if b.Clicked() {
			a.confirm = confirmState{open: true, name: s.Name}
		}
	}
	myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		switch s.State {
		case store.StateEnabled:
			// 检查更新：预览上游差异，弹出评审面板（不直接改盘）。
			ub := myui.PrimaryButton(c, "").Label("详情:检查更新").Disabled(a.hasCheckTarget())
			ub.Children(func() {
				myui.Icon(c, iconUpdate)
				myui.Text(c, "检查更新")
			})
			if ub.Clicked() {
				a.CheckAsync(s.Name)
			}
			if myui.Button(c, "停用").Clicked() {
				a.ToggleAsync(s.Name, false)
			}
			trashButton()
		case store.StateDisabled:
			if myui.PrimaryButton(c, "启用").Clicked() {
				a.ToggleAsync(s.Name, true)
			}
			trashButton()
		case store.StateTrashed:
			rb := myui.PrimaryButton(c, "").Label("恢复到技能目录")
			rb.Children(func() {
				myui.Icon(c, iconRestore)
				myui.Text(c, "恢复到技能目录")
			})
			if rb.Clicked() {
				a.RestoreAsync(s.Name)
			}
		}
	})
}

// viewStatusBar 是中栏底部的状态条：最近一次操作的结果 + 当前目录的总数。
func (a *App) viewStatusBar(c *myui.Context) {
	t := c.Theme()
	myui.Row(c).FillWidth().Padding(t.Space(1.5), t.Space(4)).
		Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		if a.status != "" {
			color := t.TextMuted
			if a.statusErr {
				color = t.Danger
			}
			myui.Text(c, a.status).SingleLine().Grow(1).MinWidth(0).TextColor(color)
		} else {
			myui.Spacer(c)
		}
		myui.Text(c, fmt.Sprintf("共 %d 个技能", len(a.skills))).
			SingleLine().Shrink(0).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
	})
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

// reviewHasChanges 报告全部检查汇总里是否存在任何可应用的变更。
func reviewHasChanges(entries []skillReviewEntry) bool {
	for _, e := range entries {
		if e.preview != nil && e.preview.HasChanges() {
			return true
		}
	}
	return false
}
