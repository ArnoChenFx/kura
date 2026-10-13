package ui

// 安装技能弹窗：从 GitHub 仓库或本地文件夹把技能装进当前技能目录。
// 安装（git clone / 复制 / 写元数据）在后台执行，完成后重新扫描并选中
// 新装的技能；失败时弹窗保持打开并显示原因，用户改完可以再试。

import (
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
}

// OpenInstall 打开安装弹窗；保留上次填写的内容，清掉旧的错误。
func (a *App) OpenInstall() {
	a.install.open, a.install.err = true, ""
}

// installOptions 把表单换成 store.AddOptions。GitHub 仓库支持直接粘贴
// https://github.com/owner/repo[.git][/tree/<ref>/<子目录>]，表单里单独填的
// 子目录与分支优先。
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
func parseRepo(in string) (repo, ref, sub string) {
	s := strings.TrimSpace(in)
	for _, p := range []string{"https://", "http://", "www.", "github.com/"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return s, "", ""
	}
	repo = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		ref = parts[3]
		sub = strings.Join(parts[4:], "/")
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

// InstallAsync 在后台把表单描述的技能装进当前目录。
func (a *App) InstallAsync() {
	s := &a.install
	if s.busy || !s.installReady() {
		return
	}
	s.busy, s.err = true, ""
	opts := s.installOptions()
	cfg, gen := a.cfg, a.gen
	a.status, a.statusErr = "正在安装…", false
	a.spawn(func() {
		res, err := cfg.Add(opts)
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			s.busy = false
			if err != nil {
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
							Placeholder("可选，如 skills/tdd").Label("子目录")
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
				myui.Text(c, "会用本机的 git 克隆仓库；安装后在卡片上可以检查更新。").
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
