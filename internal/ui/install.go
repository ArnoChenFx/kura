package ui

// 安装技能弹窗：从 GitHub 仓库或本地文件夹把技能装进当前技能目录。
// 安装（git clone / 复制 / 写元数据）在后台执行，完成后重新扫描并选中
// 新装的技能；失败时弹窗保持打开并显示原因，用户改完可以再试。
//
// 仓库里有多个候选时列出候选让用户勾选（可多选），主按钮一次把勾选的
// 全部装上：走 store.AddMany，同一个仓库只 clone 一次（见 InstallSelectedAsync）。

import (
	"errors"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// 安装来源（Segmented 的下标）。
const (
	installGitHub = iota
	installLocal
)

// installCandRows 是候选列表滚动区最多显示的行数：38 个技能的仓库
// （mattpocock/skills）不滚放不下，会把主按钮顶出窗口。
const installCandRows = 8

// installCandidate 是多候选仓库里的一个技能候选。
type installCandidate struct {
	path string // 仓库内的技能子目录（相对仓库根的 slash 路径）
	on   bool   // 是否被勾选（默认全不选）
}

// installState 是安装弹窗的表单与进度。
type installState struct {
	open    bool
	source  int    // installGitHub / installLocal
	repo    string // owner/repo，或粘贴的 GitHub 地址
	subPath string // 仓库内的技能目录（可选）
	ref     string // 分支/标签/commit（可选）
	local   string // 本地技能文件夹
	name    string // 安装后的名字（可选）
	busy    bool   // 正在安装
	picking bool   // 正在选择本地文件夹
	err     string // 上一次安装失败的原因（批量安装时是逐项失败的明细）
	// cands 是多候选仓库里待选的技能（相对仓库根的 slash 路径，已排序，
	// 默认全不选）。由安装时的自动发现填入：用户只给了 owner/repo 且仓库
	// 里有多个技能时弹窗保持打开、列出候选；勾选后点主按钮一次装完
	// （见 InstallSelectedAsync），全部成功时清空。
	cands []installCandidate
	// candRepo/candRef 是发现这批候选时表单里的仓库与 ref（规范化后的
	// owner/repo 与 ref，见 installOptions）。候选只对它们有效：用户改了
	// 仓库或分支后旧候选必须失效（见 candsLive），否则会拿旧仓库的路径
	// 去装新仓库，界面上的列表与实际目标脱钩。
	candRepo, candRef string
}

// OpenInstall 打开安装弹窗；保留上次填写的内容，清掉旧的错误。
func (a *App) OpenInstall() {
	a.install.open, a.install.err = true, ""
}

// candidatesFromPaths 把发现出来的子目录路径列表变成候选（全部未勾选）。
func candidatesFromPaths(paths []string) []installCandidate {
	out := make([]installCandidate, len(paths))
	for i, p := range paths {
		out[i] = installCandidate{path: p}
	}
	return out
}

// selected 返回被勾选的候选（保持列表顺序）。
func (s *installState) selected() []installCandidate {
	var out []installCandidate
	for _, c := range s.cands {
		if c.on {
			out = append(out, c)
		}
	}
	return out
}

// allSelected 报告候选是否全部勾选（空列表为 false，让切换按钮在空列表上
// 也是「全选」）。
func (s *installState) allSelected() bool {
	if len(s.cands) == 0 {
		return false
	}
	for _, c := range s.cands {
		if !c.on {
			return false
		}
	}
	return true
}

// toggleAll 在「全选」与「全不选」之间切换。
func (s *installState) toggleAll() {
	on := !s.allSelected()
	for i := range s.cands {
		s.cands[i].on = on
	}
}

// keepFailed 只保留路径在 failed 里的候选，并清掉它们的勾选：批量安装后
// 装成功的技能已经落地，不必再显示；失败的留下列表，用户看清原因、
// 处理完（比如改安装名称、清掉同名技能）再重新勾选。
func (s *installState) keepFailed(failed map[string]bool) {
	var keep []installCandidate
	for _, c := range s.cands {
		if failed[c.path] {
			c.on = false
			keep = append(keep, c)
		}
	}
	s.cands = keep
}

// manualSubPath 报告用户是否在「子目录」里填了具体的技能目录。填了就是
// 「装这一个」（与候选列表无关）：发现在子目录为空时才触发，但用户在候选
// 出来后仍可能改填它，这条路径的行为不能变。
func (s *installState) manualSubPath() bool {
	return strings.TrimSpace(s.subPath) != ""
}

// clearCandidates 丢掉候选列表、勾选状态，以及它们对应的仓库记录（自发现
// 那一刻起就没用了）。只在被处理的动作里调用，不在渲染路径里改状态。
func (s *installState) clearCandidates() {
	s.cands, s.candRepo, s.candRef = nil, "", ""
}

// candsLive 报告候选列表是否还对当前表单有效：候选只对「发现它们的那个仓库
// + 那个 ref」有效（同一个仓库里发现出来的路径，装到别的仓库上必然报
// 「仓库内不存在目录」）。
//
// 判据是纯比较（当前表单规范化后的 repo/ref 与发现时的记录相等），不是
// 「输入框 Changed 就清候选」：仓库输入框每帧重建，Changed 会随每次击键
// 触发，仓库名打到一半就把候选清掉会让列表乱抖；而且用户在列表里勾选时
// 表单没变，候选也不该动。所以这里只判断，真正的清空发生在动作里
// （submitInstall / InstallSelectedAsync）。附带好处：用户把仓库改回原值，
// 候选与勾选自然又有效。
func (s *installState) candsLive() bool {
	if len(s.cands) == 0 {
		return false
	}
	opts := s.installOptions()
	// Repo 为空（本地文件夹页签、仓库名还没填全）时没有「发现它的仓库」。
	return opts.Repo != "" && opts.Repo == s.candRepo && opts.Ref == s.candRef
}

// submitInstall 是安装弹窗主按钮的动作：候选列表对当前表单有效就装勾选的，
// 否则按表单重新走（发现 / 单装）。失效的候选在这里——动作里——丢掉：
// 用户改完仓库再点一下「安装」，就是「重新发现」那一下。
func (a *App) submitInstall() {
	s := &a.install
	if s.busy {
		return
	}
	if len(s.cands) > 0 && !s.candsLive() {
		s.clearCandidates()
	}
	// 手填子目录就装那一个（与候选列表无关）。
	if len(s.cands) > 0 && !s.manualSubPath() {
		a.InstallSelectedAsync()
		return
	}
	a.InstallAsync()
}

// canInstall 报告主按钮是否可以点：装勾选的候选时看勾选数，其余情况看
// 表单填够了没有。
func (s *installState) canInstall() bool {
	if s.busy {
		return false
	}
	if s.candsLive() && !s.manualSubPath() {
		return len(s.selected()) > 0
	}
	return s.installReady()
}

// installButtonLabel 是主按钮的文案：装勾选的候选时随勾选数变化（0 个时
// 写「安装选中的技能」，N 个时写「安装选中的 N 个」）；单个安装（手填
// 子目录、本地文件夹、唯一候选）与候选失效后维持原来的「安装」。
func installButtonLabel(s *installState) string {
	if !s.candsLive() || s.manualSubPath() {
		return i18n.T("ui.install.button")
	}
	n := len(s.selected())
	if n == 0 {
		return i18n.T("ui.install.buttonSelect")
	}
	return i18n.Tf("ui.install.buttonSelected", n)
}

// candidateLabel 是给用户看的技能标识：仓库根本身是技能时子目录为空。
func candidateLabel(sub string) string {
	if sub == "" {
		return i18n.T("ui.install.repoRoot")
	}
	return sub
}

// installOptions 把表单换成 store.AddOptions。GitHub 仓库支持直接粘贴
// https://github.com/owner/repo[.git][/tree/<ref>/<子目录>]，或指向技能里
// SKILL.md 的 /blob/<ref>/<子目录>/SKILL.md；表单里单独填的子目录与分支优先。
func (s *installState) installOptions() *store.AddOptions {
	opts := &store.AddOptions{Name: strings.TrimSpace(s.name)}
	if s.source == installLocal {
		opts.LocalPath = strings.TrimSpace(s.local)
		return opts
	}
	repo, ref, sub := parseRepo(s.repo)
	opts.Repo, opts.Ref, opts.SubPath = repo, ref, sub
	if v := strings.Trim(strings.TrimSpace(s.ref), "/"); v != "" {
		opts.Ref = v
	}
	if v := strings.Trim(strings.TrimSpace(s.subPath), "/"); v != "" {
		opts.SubPath = v
	}
	return opts
}

// parseRepo 从 owner/repo 或 GitHub 网址里取出仓库、ref 与子目录。
// tree 链接指向目录，子目录就是链接里的路径；blob 链接指向文件（通常是
// .../SKILL.md），子目录取文件所在的目录。浏览器地址栏里常带的 ?plain=1、
// #L10 这类查询与锚点会先去掉。
func parseRepo(in string) (repo, ref, sub string) {
	s := strings.TrimSpace(in)
	for _, p := range []string{"https://", "http://", "www.", "github.com/"} {
		s = strings.TrimPrefix(s, p)
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return s, "", ""
	}
	repo = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		ref = parts[3]
		path := parts[4:]
		// 文件名不是目录：不丢掉的话它会被拼进 SubPath，安装必然报目录不存在。
		if parts[2] == "blob" && len(path) > 0 {
			path = path[:len(path)-1]
		}
		sub = strings.Join(path, "/")
	}
	return repo, ref, sub
}

// installReady 报告表单是否填够了可以点「安装」。
func (s *installState) installReady() bool {
	if s.source == installLocal {
		return strings.TrimSpace(s.local) != ""
	}
	repo, _, _ := parseRepo(s.repo)
	return strings.Count(repo, "/") == 1 && !strings.HasSuffix(repo, "/")
}

// InstallAsync 在后台把表单描述的技能装进当前目录。仓库里有多个候选时
// 弹窗保持打开并展示候选列表，等用户选中后再装。
func (a *App) InstallAsync() {
	s := &a.install
	if s.busy || !s.installReady() {
		return
	}
	// 发现结果只在「用户没填子目录」时产生：填了就直接走安装路径。
	if s.source == installGitHub && strings.TrimSpace(s.subPath) == "" {
		_, _, sub := parseRepo(s.repo)
		if sub == "" {
			// 子目录既没填也没从链接解析出来：先发现。
			a.DiscoverInstallCandidatesAsync()
			return
		}
	}
	startInstall(a, s.installOptions())
}

// startInstall 真正发起单个技能的安装（installOptions 已就绪）。完成后重扫
// 并选中新装的技能；失败时弹窗保持打开并显示原因。cfg/gen 在发起时记下：
// 用户中途切换目录后，旧结果不再覆盖新目录的列表（见 reload）。
func startInstall(a *App, opts *store.AddOptions) {
	s := &a.install
	s.busy, s.err = true, ""
	cfg, gen := a.cfg, a.gen
	a.status, a.statusErr = i18n.T("ui.status.installing"), false
	a.spawn(func() {
		res, err := cfg.Add(opts)
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			s.busy = false
			if err != nil {
				// 多候选：弹窗保持打开，列出候选让用户挑；其余错误只报原因。
				var ce *store.CandidatesError
				if errors.As(err, &ce) {
					s.cands = candidatesFromPaths(ce.Paths)
					s.candRepo, s.candRef = opts.Repo, opts.Ref
					s.err = ""
					a.status, a.statusErr = i18n.Tf("ui.status.candidates", len(ce.Paths)), true
					return
				}
				s.err = err.Error()
				a.status, a.statusErr = i18n.T("ui.status.installFailed"), true
				return
			}
			*s = installState{source: s.source}
			if gen == a.gen {
				a.applyScan(skills, scanErr)
				a.selName, a.filter, a.query = res.ID, filterAll, ""
			}
			a.status, a.statusErr = i18n.Tf("ui.status.installed", res.ID), false
		})
	})
}

// InstallSelectedAsync 一次安装候选列表里勾选的技能：走 store.AddMany，
// 同一个仓库只 clone 一次、一次稀疏检出全部选中的目录。装的过程中 busy
// 语义与单个安装相同（spinner + 按钮禁用）；结果逐项汇总：状态行给一句话
// 摘要，失败明细留在弹窗的 err 里。
func (a *App) InstallSelectedAsync() {
	s := &a.install
	if s.busy || len(s.cands) == 0 {
		return
	}
	if !s.candsLive() {
		// 防误装：候选不属于当前表单（用户改了仓库或分支）时，绝不拿它们
		// 去装别的仓库。丢掉后按表单重新走一遍（与点主按钮等价）。
		s.clearCandidates()
		a.DiscoverInstallCandidatesAsync()
		return
	}
	sel := s.selected()
	if len(sel) == 0 {
		return
	}
	specs := make([]store.AddSpec, len(sel))
	for i, c := range sel {
		specs[i] = store.AddSpec{SubPath: c.path}
	}
	// 「安装名称」只对勾选一个时说得通：勾选多个时每个技能的默认名取它
	// 自己的 SKILL.md（见 store.installDir），这个输入框派不上用场。
	if len(specs) == 1 {
		specs[0].Name = strings.TrimSpace(s.name)
	}
	// 用与 installOptions 相同的解析（表单里的分支优先，其次链接里的）。
	repo, ref, _ := parseRepo(s.repo)
	if v := strings.Trim(strings.TrimSpace(s.ref), "/"); v != "" {
		ref = v
	}
	s.busy, s.err = true, ""
	cfg, gen := a.cfg, a.gen
	a.status, a.statusErr = i18n.T("ui.status.installing"), false
	a.spawn(func() {
		results := cfg.AddMany(repo, ref, specs)
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			s.busy = false
			var okCount, failCount int
			var lastID string
			var failed []string
			failedPaths := map[string]bool{}
			for _, r := range results {
				if r.Err != nil {
					failCount++
					failed = append(failed, i18n.Tf("ui.install.failItem", candidateLabel(r.SubPath), r.Err.Error()))
					failedPaths[r.SubPath] = true
					continue
				}
				okCount++
				lastID = r.Name
			}
			if failCount == 0 {
				// 全部成功：照旧清空表单（assign 会把 open 置回 false）、
				// 重扫并选中最后一个装上的技能。
				*s = installState{source: s.source}
				if gen == a.gen {
					a.applyScan(skills, scanErr)
					a.selName, a.filter, a.query = lastID, filterAll, ""
				}
				if okCount == 1 {
					a.status, a.statusErr = i18n.Tf("ui.status.installed", lastID), false
				} else {
					a.status, a.statusErr = i18n.Tf("ui.status.installedCount", okCount), false
				}
				return
			}
			// 有失败：弹窗保持打开。装成功的候选从列表里去掉、剩下的清掉
			// 勾选，逐项明细进 s.err 让用户看得出哪个失败、为什么。
			s.keepFailed(failedPaths)
			s.err = strings.Join(failed, "\n")
			if gen == a.gen {
				a.applyScan(skills, scanErr)
			}
			if okCount == 0 {
				a.status, a.statusErr = i18n.T("ui.status.installFailed"), true
			} else {
				a.status, a.statusErr = i18n.Tf("ui.status.installedPartial", okCount, failCount), true
			}
		})
	})
}

// DiscoverInstallCandidatesAsync 在后台 clone（复用 Add 的 clone 逻辑）并
// 发现仓库内的技能，把候选填进弹窗。唯一候选时直接填进子目录并接着安装，
// 多候选展示选择列表，零个或 clone 失败显示原因。
//
// 发现在后台跑，用户完全可能在途中改「仓库」/「分支」。完成回调先拿
// 「发起时的 repo/ref」与当前表单比对，不一致就把这次结果整份丢掉（见下面
// 回调里的注释）：绝不能拿 A 候选去装 B，也绝不能自动发起安装。
func (a *App) DiscoverInstallCandidatesAsync() {
	s := &a.install
	if s.busy {
		return
	}
	// 从表单当前值取出 repo 与 ref（installOptions 也顺带校验格式）；
	// 发现只读，不落盘，clone 与清理复用 resolveGitHub 的路径。
	opts := s.installOptions()
	repo, ref := opts.Repo, opts.Ref
	cfg, gen := a.cfg, a.gen
	s.busy, s.err = true, ""
	a.status, a.statusErr = i18n.T("ui.status.discovering"), false
	a.spawn(func() {
		cands, err := cfg.DiscoverRepoSkills(repo, ref)
		a.publish(func() {
			s.busy = false
			if gen != a.gen {
				return // 用户已切换目录，丢弃过期结果
			}
			// 这次发现只对「发起时的表单」有效：期间用户改了仓库或分支，结果整份
			// 作废——不填候选、不写子目录、不自动安装、不报错，等用户下一次动作
			// 按新表单重新发现。判据是「发起时的 repo/ref」显式比较，不是
			// candsLive()：单候选这条路径上 s.cands 是空的，candsLive() 恒为假，
			// 判不了「是不是同一个仓库」。
			if cur := s.installOptions(); cur.Repo != repo || cur.Ref != ref {
				a.status, a.statusErr = i18n.T("ui.status.repoChanged"), false
				return
			}
			if err != nil {
				s.err = err.Error()
				a.status, a.statusErr = i18n.T("ui.status.discoveryFailed"), true
				return
			}
			if len(cands) == 1 {
				if s.manualSubPath() {
					// 用户在发现期间自己填了子目录：他的输入优先，不覆盖、不自动装
					// （「手填子目录就装那一个」这条既有行为）。
					a.status, a.statusErr = i18n.T("ui.status.subPathFilled"), false
					return
				}
				// 唯一候选：填进子目录直接装，用户不用再点一次。
				s.subPath = cands[0]
				a.status = i18n.Tf("ui.status.skillFound", cands[0])
				startInstall(a, s.installOptions())
				return
			}
			if len(cands) == 0 {
				// 零候选：报清楚原因，弹窗保持打开让用户改输入。
				s.err = i18n.T("ui.install.noCandidates")
				a.status, a.statusErr = i18n.T("ui.status.discoveryFailed"), true
				return
			}
			s.cands = candidatesFromPaths(cands)
			s.candRepo, s.candRef = repo, ref
			a.status, a.statusErr = i18n.Tf("ui.status.candidates", len(cands)), false
		})
	})
}

// PickInstallFolderAsync 弹出文件夹选择框，把结果填进本地来源的路径。
func (a *App) PickInstallFolderAsync() {
	s := &a.install
	if s.picking {
		return
	}
	s.picking = true
	a.spawn(func() {
		dir, err := a.pickFolder()
		a.publish(func() {
			s.picking = false
			switch {
			case err != nil:
				s.err = i18n.Tf("ui.status.pickFolderFailed", err.Error())
			case dir != "":
				s.local = dir
			}
		})
	})
}

// viewInstallModal 是安装弹窗：
//
//	安装技能到 .agents/skills
//	[ GitHub | 本地文件夹 ]
//	仓库      [owner/repo 或 GitHub 网址      ]
//	子目录    [skills/xxx（可选）              ]
//	分支/标签 [默认分支                        ]
//	安装名称  [默认取 SKILL.md 的 name          ]
//	（多候选时：共 N 个技能  [全选] + 每行一个复选框）
//	（错误）
//	                              [取消] [安装]
func (a *App) viewInstallModal(c *myui.Context) {
	s := &a.install
	t := c.Theme()
	myui.Modal(c, &s.open, func() {
		myui.Column(c).Width(520).Gap(t.Space(3)).Children(func() {
			myui.Column(c).Gap(t.Space(1)).Children(func() {
				myui.Text(c, i18n.T("ui.action.install")).FontSize(t.FontSize + 3).Bold()
				myui.Text(c, i18n.Tf("ui.install.subtitle", a.cfg.Root)).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			myui.Segmented(c, &s.source, i18n.T("ui.install.sourceGitHub"), i18n.T("ui.install.sourceLocal"))
			myui.Form(c, func() {
				if s.source == installLocal {
					myui.Field(c, i18n.T("ui.install.folder"), func() {
						myui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
							myui.TextInput(c, &s.local).Background(a.pal.canvas).Grow(1).MinWidth(0).
								Placeholder(i18n.T("ui.install.folderPlaceholder")).Label(i18n.T("ui.install.sourceLocal"))
							if myui.Button(c, i18n.T("ui.install.choose")).Disabled(s.picking).Clicked() {
								a.PickInstallFolderAsync()
							}
						})
					})
				} else {
					myui.Field(c, i18n.T("ui.install.repo"), func() {
						myui.TextInput(c, &s.repo).Background(a.pal.canvas).FillWidth().
							Placeholder(i18n.T("ui.install.repoPlaceholder")).Label(i18n.T("ui.install.repo"))
					})
					myui.Field(c, i18n.T("ui.install.subPath"), func() {
						myui.TextInput(c, &s.subPath).Background(a.pal.canvas).FillWidth().
							Placeholder(i18n.T("ui.install.subPathPlaceholder")).Label(i18n.T("ui.install.subPath"))
					})
					myui.Field(c, i18n.T("ui.install.ref"), func() {
						myui.TextInput(c, &s.ref).Background(a.pal.canvas).FillWidth().
							Placeholder(i18n.T("ui.install.refPlaceholder")).Label(i18n.T("ui.install.ref"))
					})
				}
				myui.Field(c, i18n.T("ui.install.name"), func() {
					myui.TextInput(c, &s.name).Background(a.pal.canvas).FillWidth().
						Placeholder(i18n.T("ui.install.namePlaceholder")).Label(i18n.T("ui.install.name"))
				})
			})
			if s.source == installGitHub {
				hint := i18n.T("ui.install.hintGitHub")
				if len(s.cands) > 0 {
					hint = i18n.T("ui.install.hintCandidates")
				}
				myui.Text(c, hint).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
			if s.candsLive() {
				a.viewInstallCandidates(c)
			} else if len(s.cands) > 0 && s.source == installGitHub {
				// 候选还在内存里、但已不对当前表单有效（用户改了仓库/分支）：
				// 不显示它们，下一次点「安装」会按表单重新发现（见 candsLive）。
				myui.Text(c, i18n.T("ui.install.repoOrBranchChanged")).
					FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
			if s.err != "" {
				myui.Box(c).FillWidth().Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
					Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.4)).Children(func() {
					myui.Text(c, s.err).TextColor(t.Danger)
				})
			}
			myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
				if s.busy {
					myui.Spinner(c)
					myui.Text(c, i18n.T("ui.status.installing")).TextColor(t.TextMuted)
				}
				myui.Spacer(c)
				if myui.Button(c, i18n.T("ui.common.cancel")).Disabled(s.busy).Clicked() {
					s.open = false
				}
				// 无障碍名称写成「确认安装」，与详情栏里的「安装」时间字段区分。
				ib := myui.PrimaryButton(c, installButtonLabel(s)).Label(i18n.T("ui.install.confirmA11y")).Disabled(!s.canInstall())
				if ib.Clicked() {
					a.submitInstall()
				}
			})
		})
	})
}

// viewInstallCandidates 是多候选仓库里的选择列表：一行一个复选框
// （默认全不选），顶部是「全选 / 全不选」切换与总数。列表放在滚动容器里：
// 38 个技能的仓库（mattpocock/skills）不滚放不下，会把主按钮顶出窗口。
func (a *App) viewInstallCandidates(c *myui.Context) {
	s := &a.install
	t := c.Theme()
	rows := len(s.cands)
	if rows > installCandRows {
		rows = installCandRows
	}
	myui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		myui.Row(c).FillWidth().AlignItems(myui.Center).Children(func() {
			myui.Text(c, i18n.Tf("ui.install.total", len(s.cands))).
				FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Spacer(c)
			label := i18n.T("ui.install.selectAll")
			if s.allSelected() {
				label = i18n.T("ui.install.selectNone")
			}
			if myui.Button(c, label).Clicked() {
				s.toggleAll()
				c.Invalidate()
			}
		})
		myui.Scroll(c).FillWidth().Height(float32(rows) * t.Space(5)).Children(func() {
			myui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
				for i := range s.cands {
					// 直接绑应用状态（a.install.cands[i].on）：复选框的输入
					// 在构建时应用，Changed 为 true 时 on 已是新值，主按钮的
					// 文案与禁用状态（在下面构建）同一帧就跟上了。
					cnd := &s.cands[i]
					if myui.Checkbox(c, &cnd.on, cnd.path).Changed() {
						c.Invalidate()
					}
				}
			})
		})
	})
}
