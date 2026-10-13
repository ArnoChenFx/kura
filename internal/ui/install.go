package ui

// 安装技能弹窗：从 GitHub 仓库或本地文件夹把技能装进当前技能目录。
// 安装（git clone / 复制 / 写元数据）在后台执行，完成后重新扫描并选中
// 新装的技能；失败时弹窗保持打开并显示原因，用户改完可以再试。

import (
	"errors"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// 安装来源（Segmented 的下标）。
const (
	installGitHub = iota
	installLocal
)

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
	err     string // 上一次安装失败的原因
	// cands 是多候选仓库里待选的技能子目录（相对仓库根的 slash 路径，
	// 已排序）。由安装时的自动发现填入：用户只给了 owner/repo 且仓库
	// 里有多个技能时弹窗保持打开、列出候选，点某个候选填进子目录。
	// 选择后（ChooseInstallCandidate）与安装成功后清空。
	cands []string
}

// OpenInstall 打开安装弹窗；保留上次填写的内容，清掉旧的错误。
func (a *App) OpenInstall() {
	a.install.open, a.install.err = true, ""
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

// startInstall 真正发起安装（installOptions 已就绪）。完成后重扫并选中
// 新装的技能；失败时弹窗保持打开并显示原因。cfg/gen 在发起时记下：
// 用户中途切换目录后，旧结果不再覆盖新目录的列表（见 reload）。
func startInstall(a *App, opts *store.AddOptions) {
	s := &a.install
	s.busy, s.err = true, ""
	cfg, gen := a.cfg, a.gen
	a.status, a.statusErr = "正在安装…", false
	a.spawn(func() {
		res, err := cfg.Add(opts)
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			s.busy = false
			if err != nil {
				// 多候选：弹窗保持打开，列出候选让用户挑；其余错误只报原因。
				var ce *store.CandidatesError
				if errors.As(err, &ce) {
					s.cands = ce.Paths
					s.err = ""
					a.status, a.statusErr = "仓库里有 "+itoa(len(ce.Paths))+" 个技能，请选择要安装的", true
					return
				}
				s.err = err.Error()
				a.status, a.statusErr = "安装失败", true
				return
			}
			*s = installState{source: s.source}
			if gen == a.gen {
				a.applyScan(skills, scanErr)
				a.selName, a.filter, a.query = res.ID, filterAll, ""
			}
			a.status, a.statusErr = "已安装 "+res.ID, false
		})
	})
}

// DiscoverInstallCandidatesAsync 在后台 clone（复用 Add 的 clone 逻辑）并
// 发现仓库内的技能，把候选填进弹窗。唯一候选时直接填进子目录并接着安装，
// 多候选展示选择列表，零个或 clone 失败显示原因。
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
	a.status, a.statusErr = "正在查找仓库里的技能…", false
	a.spawn(func() {
		cands, err := cfg.DiscoverRepoSkills(repo, ref)
		a.publish(func() {
			s.busy = false
			if gen != a.gen {
				return // 用户已切换目录，丢弃过期结果
			}
			if err != nil {
				s.err = err.Error()
				a.status, a.statusErr = "查找失败", true
				return
			}
			if len(cands) == 1 {
				// 唯一候选：填进子目录直接装，用户不用再点一次。
				s.subPath = cands[0]
				a.status = "发现技能 " + cands[0]
				startInstall(a, s.installOptions())
				return
			}
			if len(cands) == 0 {
				// 零候选：报清楚原因，弹窗保持打开让用户改输入。
				s.err = "store: 仓库内未找到含 SKILL.md 的技能目录"
				a.status, a.statusErr = "查找失败", true
				return
			}
			s.cands = cands
		})
	})
}

// ChooseInstallCandidate 把选中的候选填进「子目录」输入框并清掉候选列表，
// 之后用户点「安装」（或回车）即按显式子目录安装。
func (a *App) ChooseInstallCandidate(path string) {
	s := &a.install
	s.subPath, s.cands, s.err = path, nil, ""
	a.status = "已选择 " + path
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
				s.err = "选择文件夹失败: " + err.Error()
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
//	安装名称  [默认取来源文件夹名               ]
//	（错误）
//	                              [取消] [安装]
func (a *App) viewInstallModal(c *myui.Context) {
	s := &a.install
	t := c.Theme()
	myui.Modal(c, &s.open, func() {
		myui.Column(c).Width(520).Gap(t.Space(3)).Children(func() {
			myui.Column(c).Gap(t.Space(1)).Children(func() {
				myui.Text(c, "安装技能").FontSize(t.FontSize + 3).Bold()
				myui.Text(c, "安装到 "+a.cfg.Root).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			myui.Segmented(c, &s.source, "GitHub 仓库", "本地文件夹")
			myui.Form(c, func() {
				if s.source == installLocal {
					myui.Field(c, "文件夹", func() {
						myui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
							myui.TextInput(c, &s.local).Background(a.pal.canvas).Grow(1).MinWidth(0).
								Placeholder("包含 SKILL.md 的文件夹").Label("本地文件夹")
							if myui.Button(c, "选择…").Disabled(s.picking).Clicked() {
								a.PickInstallFolderAsync()
							}
						})
					})
				} else {
					myui.Field(c, "仓库", func() {
						myui.TextInput(c, &s.repo).Background(a.pal.canvas).FillWidth().
							Placeholder("owner/repo，或粘贴 GitHub 网址").Label("仓库")
					})
					myui.Field(c, "子目录", func() {
						myui.TextInput(c, &s.subPath).Background(a.pal.canvas).FillWidth().
							Placeholder("可选，留空自动发现").Label("子目录")
					})
					myui.Field(c, "分支/标签", func() {
						myui.TextInput(c, &s.ref).Background(a.pal.canvas).FillWidth().
							Placeholder("可选，默认分支").Label("分支/标签")
					})
				}
				myui.Field(c, "安装名称", func() {
					myui.TextInput(c, &s.name).Background(a.pal.canvas).FillWidth().
						Placeholder("可选，默认取来源文件夹名").Label("安装名称")
				})
			})
			if s.source == installGitHub {
				hint := "会用本机的 git 克隆仓库；子目录留空时自动发现仓库里的技能。"
				if len(s.cands) > 0 {
					hint = "仓库里有多个技能，点击下面某个候选把它填进子目录。"
				}
				myui.Text(c, hint).
					FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
			if len(s.cands) > 0 {
				a.viewInstallCandidates(c)
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
					myui.Text(c, "正在安装…").TextColor(t.TextMuted)
				}
				myui.Spacer(c)
				if myui.Button(c, "取消").Disabled(s.busy).Clicked() {
					s.open = false
				}
				// 无障碍名称写成「确认安装」，与详情栏里的「安装」时间字段区分。
				ib := myui.PrimaryButton(c, "安装").Label("确认安装").Disabled(s.busy || !s.installReady())
				if ib.Clicked() {
					a.InstallAsync()
				}
			})
		})
	})
}

// viewInstallCandidates 是多候选仓库里的选择列表：一行一个候选，点击即
// 填进「子目录」输入框。用 ButtonBase 自建行（Hovered/Clicked，同 rootItem
// 的写法），不用 RightClicked：行要收左键点击。
func (a *App) viewInstallCandidates(c *myui.Context) {
	s := &a.install
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
		for _, p := range s.cands {
			b := myui.ButtonBase(c).Key(p).Label(p).FillWidth().Justify(myui.Start).
				Padding(t.Space(1), t.Space(2)).Radius(t.Radius - 2).
				TextColor(a.pal.textMid).Children(func() {
				myui.Icon(c, iconFolder).FontSize(t.FontSize)
				myui.Text(c, p).SingleLine().Grow(1).MinWidth(0).FontSize(t.FontSize - 1)
			})
			// 选中的候选高亮，给用户一个「当前会装哪个」的反馈。
			switch {
			case s.subPath == p:
				b.Background(a.pal.sel).TextColor(t.Text)
			case b.Hovered():
				b.Background(t.SurfaceHover).TextColor(t.Text)
			}
			if b.Clicked() {
				a.ChooseInstallCandidate(p)
				c.Invalidate()
			}
		}
	})
}
