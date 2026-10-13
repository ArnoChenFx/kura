package ui

import (
	"fmt"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// listWidth 是左栏（列表）的固定宽度。足够放下名字与第二行说明，
// 又把窗口的大部分留给详情面板。
const listWidth = 300

// View 构建 kura 主窗口的一帧，结构自上而下：
//
//	┌────────────────────────────────────────────────┐
//	│ 头部：kura · 技能根目录路径      [启用 a][停用 b][回收站 c] │
//	│ 工具栏：🔍 搜索……        （分隔线） 刷新|全部检查  导入元数据 │
//	│ ┌──────────────┬─────────────────────────────┐ │
//	│ │ 技能列表      │  未选中：空态（图标+计数+提示）  │ │
//	│ │ 名字          │  已选中：标题 + 状态徽标        │ │
//	│ │ ●来源·说明 ⬜ │        元数据字段（Form）       │ │
//	│ │              │        操作按钮                │ │
//	│ └──────────────┴─────────────────────────────┘ │
//	│ 状态条：最近一次操作的结果 / 错误横幅             │
//	└────────────────────────────────────────────────┘
func (a *App) View(c *myui.Context) {
	// 首帧触发一次后台初始扫描（磁盘 I/O 不在 UI 线程做）。
	if !a.loaded {
		a.loaded = true
		a.ReloadAsync()
	}
	rows := a.visible()

	myui.Column(c).Fill().Children(func() {
		a.viewHeader(c)
		a.viewToolbar(c)
		// 根目录缺失/扫描失败是全局性问题：横幅放列表上方，而不是
		// 塞在列表内部与内容争空间。
		a.viewErrorBanner(c)
		a.viewBody(c, rows)
		a.viewStatusBar(c)
	})

	// 「移入回收站」确认框；最后一个按钮是默认按钮（回车触发），
	// Escape 或点按钮后 *open 置 false。
	if n := myui.AlertDialog(c, &a.confirm.open, "移入回收站？",
		"技能 "+a.confirm.name+" 将从根目录移入 kura 回收站，可随时恢复；移动前会自动写入内容快照。",
		"取消", "确认移入"); n == 1 {
		a.RemoveAsync(a.confirm.name)
	}

	// 更新评审面板（检查更新/全部检查的结果 + 应用/取消）。
	a.viewReviewModal(c)
}

// viewHeader 是头部：左侧标题 + 根目录路径（用户此刻在看哪里），右侧
// 三个状态的计数。计数从页脚提升到这里，打开窗口即可读。
func (a *App) viewHeader(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	myui.Row(c).FillWidth().Padding(t.Space(3), t.Space(4), t.Space(2), t.Space(4)).
		Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
		myui.Text(c, "kura").Bold().FontSize(t.FontSize + 3)
		// 路径可能很长：压缩到中段，保住盘符与最后一级目录。
		myui.Row(c).AlignItems(myui.Center).Gap(t.Space(1)).Grow(1).MinWidth(0).Children(func() {
			myui.Icon(c, iconFolder).TextColor(t.TextMuted)
			myui.Text(c, elideMiddle(a.cfg.Root)).SingleLine().TextColor(t.TextMuted)
		})
		myui.Row(c).Gap(t.Space(3)).Shrink(0).AlignItems(myui.Center).Children(func() {
			countChip(c, store.StateEnabled, en)
			countChip(c, store.StateDisabled, dis)
			countChip(c, store.StateTrashed, trash)
		})
	})
}

// elideMiddle 把过长路径压缩成 D:\...\skills 的形式；短路径原样返回。
func elideMiddle(p string) string {
	const max = 52
	if len(p) <= max {
		return p
	}
	// 保留结尾 24 字节（一般是 \...\skills 这类关键信息）。
	return p[:max-3] + "…" + p[len(p)-24:]
}

// viewToolbar 是工具栏：MyGo 的 Toolbar（一行控件、溢出时自动收进菜单）。
// 搜索框是主控件（Grow 占满剩余空间，中文占位符）；刷新/全部检查/导入
// 元数据是右侧的图标次级按钮——在 Toolbar 里它们悬停才有底色，与内容
// 拉开层级。按钮用 Label 命名，给无障碍、溢出菜单与测试。
func (a *App) viewToolbar(c *myui.Context) {
	t := c.Theme()
	myui.Toolbar(c, func() {
		searchBox(c, &a.query).Grow(1).MaxWidth(t.Space(88))
		if a.scanning || a.checkingAll || a.checking != "" {
			myui.Spinner(c)
		}
		myui.Spacer(c)
		rb := myui.Button(c, "").Label("刷新").Disabled(a.scanning)
		rb.Children(func() { myui.Icon(c, iconRefresh) })
		if rb.Clicked() {
			a.ReloadAsync()
		}
		cb := myui.Button(c, "").Label("全部检查").Disabled(a.checkingAll || a.checking != "")
		cb.Children(func() { myui.Icon(c, iconCheckAll) })
		if cb.Clicked() {
			a.CheckAllAsync()
		}
		imb := myui.Button(c, "").Label("导入元数据")
		imb.Children(func() { myui.Icon(c, iconImport) })
		if imb.Clicked() {
			a.ImportAsync()
		}
	})
}

// viewErrorBanner 是全局错误横幅：根目录缺失或扫描失败时出现在列表上方。
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
	myui.Column(c).Width(listWidth).FillHeight().Padding(t.Space(2), 0, t.Space(2), t.Space(3)).
		Gap(t.Space(1)).Children(func() {
		if len(rows) == 0 {
			a.viewListEmpty(c)
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

// viewListEmpty 是列表为空时的占位：区分「搜索无结果」与「根目录没有
// 技能」。扫描失败时由上方横幅报错，这里保持确定的「没有技能」。
func (a *App) viewListEmpty(c *myui.Context) {
	t := c.Theme()
	msg := "没有技能"
	if a.query != "" {
		msg = "没有匹配「" + a.query + "」的技能"
	}
	myui.Box(c).Grow(1).Center().Children(func() {
		myui.Column(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Icon(c, iconInbox).TextColor(t.TextMuted).FontSize(t.FontSize + 10)
			myui.Text(c, msg).TextColor(t.TextMuted)
		})
	})
}

// viewSkillRow 是列表的一行，行内网格固定：
//
//	[状态点] [名字/说明两行 …… 占满] [开关或恢复按钮（右缘对齐）]
//
// 选中态与悬停态由 List 自身绘制；停用行的名字降为弱化色并加删除线，
// 回收站行再加删除线，与「在用」的技能明显区分。
func (a *App) viewSkillRow(r myui.ListRow, s *store.Skill) {
	c := r.Context
	t := c.Theme()
	busy := a.busy[s.Name]

	myui.Row(c).FillWidth().Padding(t.Space(1.5), t.Space(2)).
		Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		stateDot(c, s.State)

		// 选中行的底色是 Accent，弱化色在上面几乎不可读；选中时两行都改用
		// AccentText，副标题与状态徽标才有足够对比度。
		labelColor, mutedColor := t.Text, t.TextMuted
		if r.Selected() {
			labelColor, mutedColor = t.AccentText, t.AccentText
		}
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
			name := myui.Text(c, s.Name).SingleLine().FontWeight(600).TextColor(labelColor)
			if s.State != store.StateEnabled {
				name.TextColor(mutedColor)
			}
			if s.State == store.StateTrashed {
				name.Strikethrough()
			}
			myui.Text(c, rowCaption(s)).SingleLine().FontSize(t.FontSize - 2).
				TextColor(mutedColor)
		})

		// 右缘对齐的操作区：回收站技能只有「恢复」，其余是启用开关；
		// 忙碌时用转圈占位，保持行的宽度网格稳定。
		if busy {
			myui.Spinner(c)
			return
		}
		if s.State == store.StateTrashed {
			rb := myui.Button(c, "").Label("恢复")
			rb.Children(func() { myui.Icon(c, iconRestore) })
			if rb.Clicked() {
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

// viewDetail 是右侧详情面板。未选中时给出「有用的空态」：图标、根目录
// 路径与计数、引导文字，而不是一行漂浮的灰字。选中后按「标题 → 元数据
// → 位置 → 操作」分组展示。
func (a *App) viewDetail(c *myui.Context, rows []*store.Skill) {
	t := c.Theme()
	myui.Column(c).Grow(1).FillHeight().Padding(t.Space(3), t.Space(4)).
		Gap(t.Space(2.5)).Children(func() {
		if a.selRow < 0 || a.selRow >= len(rows) {
			a.viewDetailEmpty(c)
			return
		}
		s := rows[a.selRow]
		busy := a.busy[s.Name]

		// 标题区：名字 + 状态徽标 + 来源。
		myui.Column(c).Gap(t.Space(1.5)).Children(func() {
			name := myui.Text(c, s.Name).Bold().FontSize(t.FontSize + 5)
			if s.State != store.StateEnabled {
				name.TextColor(t.TextMuted)
			}
			if s.State == store.StateTrashed {
				name.Strikethrough()
			}
			myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
				stateBadge(c, s.State)
				myui.Text(c, sourceText(s)).SingleLine().TextColor(t.TextMuted)
				if !s.HasSKILLMD && s.State != store.StateTrashed {
					myui.Text(c, "· 缺少 SKILL.md").SingleLine().TextColor(t.Warning)
				}
			})
		})

		myui.Divider(c)

		// 元数据区：Form 让标签右对齐、跨行对齐；缺失或损坏时给出
		// 原因，绝不 panic。
		sectionHeader(c, "元数据")
		if m := s.Meta; m != nil {
			myui.Form(c, func() {
				metaField(c, "来源", sourceURLText(m), true)
				if m.Source.Type == store.SourceGitHub {
					if m.Source.Ref != "" {
						metaField(c, "Ref", m.Source.Ref, false)
					}
					if m.Source.SubPath != "" {
						metaField(c, "SubPath", m.Source.SubPath, false)
					}
					if m.Source.Commit != "" {
						metaField(c, "Commit", m.Source.Commit, false)
					}
				}
				if m.PluginName != "" {
					metaField(c, "Plugin", m.PluginName, false)
				}
				if m.InstalledAt != "" {
					metaField(c, "安装时间", m.InstalledAt, false)
				}
				if m.UpdatedAt != "" {
					metaField(c, "更新时间", m.UpdatedAt, false)
				}
				metaField(c, "内容哈希", shortHash(m.ContentHash), false)
			})
		} else if s.MetaErr != "" {
			myui.Text(c, "元数据读取失败："+s.MetaErr).TextColor(t.Danger)
		} else {
			myui.Text(c, "没有 .kura-meta.json（非 kura 安装，或尚未导入元数据）").TextColor(t.TextMuted)
		}

		// 位置区：技能目录的实际路径。只留带路径的那一行，不再另起一个小节
		// 标签，避免「位置 / 所在位置」重复。
		myui.Form(c, func() {
			metaField(c, "所在位置", s.Dir, true)
		})

		myui.Spacer(c)

		// 操作区：按状态给出可用动作，后台执行期间显示忙碌。
		a.viewDetailActions(c, s, busy)
	})
}

// viewDetailActions 是详情底部的操作行，按技能状态分组：
// 启用 → 检查更新（主，带图标）+ 停用 + 移入回收站（图标，危险色）；
// 停用 → 启用（主）+ 移入回收站（图标）；
// 回收站 → 恢复到根目录（主，带图标）。
func (a *App) viewDetailActions(c *myui.Context, s *store.Skill, busy bool) {
	t := c.Theme()
	// trashButton 是「移入回收站」的图标按钮：中性底色悬停变危险色，
	// Label 保持「详情:移入回收站」供测试与无障碍。
	trashButton := func() {
		b := myui.Button(c, "").Label("详情:移入回收站")
		b.Children(func() { myui.Icon(c, iconTrash).TextColor(t.TextMuted) })
		if b.Clicked() {
			a.confirm = confirmState{open: true, name: s.Name}
		}
	}
	if busy {
		myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, "正在操作 "+s.Name+"…").SingleLine().TextColor(t.TextMuted)
		})
		return
	}
	myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		switch s.State {
		case store.StateEnabled:
			// 检查更新：预览上游差异，弹出评审面板（不直接改盘）。
			ub := myui.PrimaryButton(c, "").Label("详情:检查更新")
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
			rb := myui.PrimaryButton(c, "").Label("恢复到根目录")
			rb.Children(func() {
				myui.Icon(c, iconRestore)
				myui.Text(c, "恢复到根目录")
			})
			if rb.Clicked() {
				a.RestoreAsync(s.Name)
			}
		}
	})
}

// viewDetailEmpty 是未选中技能时的详情空态：让这块空间讲清楚「这里在
// 管什么、有什么」，并引导下一步。
func (a *App) viewDetailEmpty(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	myui.Column(c).Grow(1).AlignItems(myui.Center).Justify(myui.Center).
		Gap(t.Space(4)).Children(func() {
		myui.Icon(c, iconInbox).TextColor(t.TextMuted).FontSize(t.FontSize * 4)
		myui.Column(c).Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
			myui.Text(c, "选择一个技能查看详情").Bold()
			myui.Text(c, "元数据、来源与更新操作会显示在这里").TextColor(t.TextMuted)
		})
		// 概况卡片：当前根目录里有什么。
		myui.Column(c).Gap(t.Space(1.5)).Padding(t.Space(3), t.Space(4)).
			Radius(t.Radius+2).Background(t.Surface).Border(1, t.Border).
			MinWidth(t.Space(60)).AlignItems(myui.Center).Children(func() {
			myui.Row(c).AlignItems(myui.Center).Gap(t.Space(1.5)).Children(func() {
				myui.Icon(c, iconFolder).TextColor(t.TextMuted)
				myui.Text(c, "技能根目录").SingleLine().FontWeight(600)
			})
			myui.Text(c, elideMiddle(a.cfg.Root)).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Divider(c)
			myui.Row(c).Gap(t.Space(4)).AlignItems(myui.Center).Children(func() {
				myui.Column(c).Gap(t.Space(0.5)).AlignItems(myui.Center).Children(func() {
					myui.Text(c, itoa(en)).Bold().TextColor(t.Success)
					myui.Text(c, "启用").FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				})
				myui.Column(c).Gap(t.Space(0.5)).AlignItems(myui.Center).Children(func() {
					myui.Text(c, itoa(dis)).Bold().TextColor(t.Warning)
					myui.Text(c, "停用").FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				})
				myui.Column(c).Gap(t.Space(0.5)).AlignItems(myui.Center).Children(func() {
					myui.Text(c, itoa(trash)).Bold().TextColor(t.Danger)
					myui.Text(c, "回收站").FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				})
			})
		})
	})
}

// viewStatusBar 是底部状态条：最近一次操作的结果 + 技能总数。
// 计数已在头部，这里只留一条安静的辅助信息。
func (a *App) viewStatusBar(c *myui.Context) {
	t := c.Theme()
	myui.Row(c).FillWidth().Padding(t.Space(1.5), t.Space(4)).
		Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		if a.status != "" {
			color := t.TextMuted
			if a.statusErr {
				color = t.Danger
			}
			myui.Text(c, a.status).SingleLine().TextColor(color)
		}
		myui.Spacer(c)
		myui.Text(c, fmt.Sprintf("共 %d 个技能", len(a.skills))).
			SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
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
