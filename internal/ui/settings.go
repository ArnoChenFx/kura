package ui

// 设置弹窗：配置 OpenAI 兼容的翻译 API（地址、密钥、模型）与全局目标语言，
// 保存到 ~/.kura/settings.json（读写经 store.LoadSettings / SaveSettings，
// 本包不直接碰磁盘）。密钥以明文保存是用户明确选择的方案，弹窗里写明。

import (
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// settingsState 是设置弹窗的状态。输入框绑定的是弹窗自己的副本：打开时
// 从 App.settings 灌入一次（OpenSettings），之后用户的编辑留在这里，
// 构建时绝不再从 App.settings 覆盖，否则输入会被抹掉。
type settingsState struct {
	open    bool
	baseURL string // API 地址
	apiKey  string // 密钥（输入框用 Password 掩盖显示）
	model   string // 模型名
	lang    string // 目标语言的可读名（targetLangs 里的 label）
	busy    bool   // 预留：保存期间禁用按钮（保存本身立即生效，一般不会置位）
	err     string // 弹窗内的错误（如目标语言不被支持）
	note    string // 打开时的提示（如已存的语言无法识别，已回到默认）
}

// targetLang 是一个目标语言：规范语言码与界面显示名。
type targetLang struct {
	code  string // 规范码，必须是 store.NormalizeTargetLang 认得（且规范化后不变）的
	label string // 界面显示名
}

// targetLangs 是设置里可选的目标语言，显式列在 UI 侧：store 的显示名表是
// 未导出的，且「选项顺序」是界面决策。每个 code 必须能被
// store.NormalizeTargetLang 原样认回，否则保存会被拒（TestTargetLangsAreNormalized 守着）。
var targetLangs = []targetLang{
	{"zh-CN", "简体中文"},
	{"en", "English"},
	{"en-US", "English (US)"},
	{"en-GB", "English (UK)"},
	{"ja", "日本語"},
	{"ko", "한국어"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"es", "Español"},
	{"pt", "Português"},
	{"ru", "Русский"},
}

// langLabels 返回 Select 用的选项（可读名，顺序同 targetLangs）。
func langLabels() []string {
	out := make([]string, len(targetLangs))
	for i, l := range targetLangs {
		out[i] = l.label
	}
	return out
}

// langLabelFor 返回规范语言码对应的显示名；不在列表里返回空串。
func langLabelFor(code string) string {
	for _, l := range targetLangs {
		if l.code == code {
			return l.label
		}
	}
	return ""
}

// langCodeFor 返回显示名对应的规范码；不认识的显示名原样返回，让
// store.NormalizeTargetLang 去拒绝它（不在这里猜）。
func langCodeFor(label string) string {
	for _, l := range targetLangs {
		if l.label == label {
			return l.code
		}
	}
	return label
}

// OpenSettings 打开设置弹窗：把当前设置灌进弹窗的输入框（只此一次），
// 清掉旧的错误提示。已存的目标语言认不得时落到默认语言并在弹窗里提示。
func (a *App) OpenSettings() {
	s := &a.settingsUI
	*s = settingsState{
		open:    true,
		baseURL: a.settings.BaseURL,
		apiKey:  a.settings.APIKey,
		model:   a.settings.Model,
	}
	code, err := store.NormalizeTargetLang(a.settings.TargetLang)
	if err != nil {
		code = store.DefaultTargetLang
		s.note = "已保存的目标语言无法识别，已回到" + langLabelFor(code) + "；保存后生效。"
	}
	s.lang = langLabelFor(code)
	if s.lang == "" {
		s.lang = targetLangs[0].label
	}
}

// settingsDraft 把弹窗里的输入换成待保存的 store.Settings（去掉首尾空白）。
func (s *settingsState) settingsDraft() store.Settings {
	return store.Settings{
		BaseURL:    strings.TrimSpace(s.baseURL),
		APIKey:     strings.TrimSpace(s.apiKey),
		Model:      strings.TrimSpace(s.model),
		TargetLang: langCodeFor(s.lang),
	}
}

// sameTranslateConfig 报告两份设置对翻译而言是否等价（不看 CreatedAt）：
// 只要接口、密钥、模型、目标语言有一项变了，旧译文就不再适用。
func sameTranslateConfig(a, b store.Settings) bool {
	return a.BaseURL == b.BaseURL && a.APIKey == b.APIKey && a.Model == b.Model && a.TargetLang == b.TargetLang
}

// SaveSettingsAsync 保存设置：
//
//   - 先在 UI 线程校验目标语言（store.NormalizeTargetLang）：不认识就把原因
//     显示在弹窗里，**不改内存、不写盘**。
//   - 通过后立刻换上内存里的设置（翻译马上用新配置）、关弹窗，写盘在后台
//     做；失败只在状态行报告，不回滚内存（同 SetThemeAsync）。
//   - 配置（接口/密钥/模型/语言）变了就清空译文缓存，并作废在途请求。
//   - 连续保存只有最新一次真正写盘，避免旧任务把新值覆盖回去。
//
// 注意写的是 <Home>/settings.json，与技能目录无关；翻译只读它。
func (a *App) SaveSettingsAsync(s store.Settings) {
	lang, err := store.NormalizeTargetLang(s.TargetLang)
	if err != nil {
		a.settingsUI.err = err.Error()
		return
	}
	s.TargetLang = lang
	s.BaseURL, s.APIKey, s.Model = strings.TrimSpace(s.BaseURL), strings.TrimSpace(s.APIKey), strings.TrimSpace(s.Model)
	if s.CreatedAt == "" {
		// 首建时间不漂移：沿用已有的（SaveSettings 只在为空时补当前时间）。
		s.CreatedAt = a.settings.CreatedAt
	}
	changed := !sameTranslateConfig(a.settings, s)
	a.settingsMu.Lock()
	a.settings = s
	a.settingsMu.Unlock()
	if changed {
		a.trans.reset()
	}
	a.settingsUI = settingsState{}
	a.status, a.statusErr = "正在保存设置…", false

	cfg := a.cfg
	seq := a.settingsSeq.Add(1)
	a.spawn(func() {
		a.settingsWriteMu.Lock()
		defer a.settingsWriteMu.Unlock()
		if a.settingsSeq.Load() != seq {
			return // 已有更新的保存排在后面，由它写最新值
		}
		out := s // SaveSettings 会补 CreatedAt：写进副本，再由 publish 带回
		err := store.SaveSettings(cfg, &out)
		a.publish(func() {
			if err != nil {
				a.status, a.statusErr = "设置没能保存："+err.Error(), true
				return
			}
			a.settingsMu.Lock()
			if a.settings.CreatedAt == "" {
				a.settings.CreatedAt = out.CreatedAt
			}
			a.settingsMu.Unlock()
			a.settingsErr, a.settingsLoaded = "", true
			if a.settings.Ready() {
				a.status, a.statusErr = "设置已保存", false
			} else {
				a.status, a.statusErr = "设置已保存，但翻译 API 还没填全，暂时不能翻译", false
			}
		})
	})
}

// viewSettingsModal 是设置弹窗：
//
//	设置
//	翻译 API（OpenAI 兼容）
//	API 地址  [https://api.openai.com/v1]
//	密钥      [••••••••]
//	模型      [gpt-4o-mini]
//	目标语言  [简体中文 ▾]
//	（说明：密钥明文保存；译文只用于界面显示）
//	（提示 / 错误）
//	                         [取消] [保存]
func (a *App) viewSettingsModal(c *myui.Context) {
	s := &a.settingsUI
	t := c.Theme()
	myui.Modal(c, &s.open, func() {
		myui.Column(c).Width(520).Gap(t.Space(3)).Children(func() {
			myui.Column(c).Gap(t.Space(1)).Children(func() {
				myui.Text(c, "设置").FontSize(t.FontSize + 3).Bold()
				myui.Text(c, "翻译 API（OpenAI 兼容）").FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			// settings.json 读不出来（JSON 损坏）：说清楚，保存会覆盖它。
			if a.settingsErr != "" {
				myui.Box(c).FillWidth().Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
					Background(t.Warning.Alpha(0.10)).Border(1, t.Warning.Alpha(0.4)).Children(func() {
					myui.Text(c, "设置文件读取失败，保存会覆盖它："+a.settingsErr).MaxLines(4).TextColor(t.Warning)
				})
			}
			myui.Form(c, func() {
				myui.Field(c, "API 地址", func() {
					myui.TextInput(c, &s.baseURL).Background(a.pal.canvas).FillWidth().
						Placeholder("https://api.openai.com/v1").Label("API 地址")
				})
				myui.Field(c, "密钥", func() {
					myui.TextInput(c, &s.apiKey).Background(a.pal.canvas).FillWidth().
						Placeholder("sk-…").Label("密钥").Password()
				})
				myui.Field(c, "模型", func() {
					myui.TextInput(c, &s.model).Background(a.pal.canvas).FillWidth().
						Placeholder("gpt-4o-mini").Label("模型")
				})
				myui.Field(c, "目标语言", func() {
					myui.Select(c, &s.lang, langLabels()).Label("目标语言")
				})
			})
			myui.Text(c, "密钥以明文保存在 ~/.kura/settings.json；译文只用于界面显示，不会改动技能文件。").
				FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			// 没填全不阻止保存（可能只想先选语言），只提醒翻译暂时用不了。
			draft := s.settingsDraft()
			if !draft.Ready() {
				myui.Text(c, "API 地址、密钥、模型都填好后，才能翻译技能描述。").
					FontSize(t.FontSize - 1).TextColor(t.Warning)
			}
			if s.note != "" {
				myui.Text(c, s.note).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
			if s.err != "" {
				myui.Box(c).FillWidth().Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
					Background(t.Danger.Alpha(0.10)).Border(1, t.Danger.Alpha(0.4)).Children(func() {
					myui.Text(c, s.err).TextColor(t.Danger)
				})
			}
			myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
				myui.Spacer(c)
				if myui.Button(c, "取消").Disabled(s.busy).Clicked() {
					s.open = false
				}
				// 无障碍名称「确认保存」，与别处文字区分（同安装弹窗）。
				if myui.PrimaryButton(c, "保存").Label("确认保存").Disabled(s.busy).Clicked() {
					a.SaveSettingsAsync(s.settingsDraft())
				}
			})
		})
	})
}
