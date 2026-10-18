package ui

// 设置弹窗：配置 OpenAI 兼容的翻译 API（地址、密钥、模型）与全局目标语言，
// 保存到 ~/.kura/settings.json（读写经 store.LoadSettings / SaveSettings，
// 本包不直接碰磁盘）。密钥以明文保存是用户明确选择的方案，弹窗里写明。

import (
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
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

// targetLang 是设置里可选的一个目标语言：规范码 + 显示名的取值函数。
type targetLang struct {
	code  string        // 规范码，必须能被 store.NormalizeTargetLang 原样认回
	label func() string // 显示名（语言的自称，两种界面语言下都不变）
}

// targetLangs 是设置里可选的目标语言，顺序即下拉顺序。每个 code 必须能被
// store.NormalizeTargetLang 原样认回，否则保存会被拒
// （TestTargetLangsAreNormalized 守着）。
//
// 这些显示名是**语言的自称**（简体中文、日本語、Français…）：说中文的人找
// 「简体中文」，说日语的人找「日本語」，所以两种界面语言下都显示自称。名字
// 仍然存在 locales 里（store 的 langLabel 用同一份，单一来源），只是不随
// 界面语言变（对它们来说，两种语言的目录取值本来就相同）。
var targetLangs = []targetLang{
	{"zh-CN", locales.StoreLangNameZhCN},
	{"en", locales.StoreLangNameEn},
	{"en-US", locales.StoreLangNameEnUS},
	{"en-GB", locales.StoreLangNameEnGB},
	{"ja", locales.StoreLangNameJa},
	{"ko", locales.StoreLangNameKo},
	{"fr", locales.StoreLangNameFr},
	{"de", locales.StoreLangNameDe},
	{"es", locales.StoreLangNameEs},
	{"pt", locales.StoreLangNamePt},
	{"ru", locales.StoreLangNameRu},
}

// ifaceLang 是「界面语言」下拉的一个选项：config.json 的 lang 偏好值 + 显示名。
type ifaceLang struct {
	pref  string        // store.LangSystem / LangZH / LangEN
	label func() string // 显示名：非自称项（跟随系统）随界面语言变
}

// ifaceLangs 是「界面语言」下拉的选项与顺序。简体中文 / English 显示自称：
// 界面语言正是用当前语言命名的最容易认不清的一处，自称让任何语言的使用者都
// 认得；只有「跟随系统」是随界面语言变的普通文案。
var ifaceLangs = []ifaceLang{
	{pref: "system", label: locales.UiSettingsLangSystem},
	{pref: "zh", label: locales.StoreLangNameZhCN},
	{pref: "en", label: locales.StoreLangNameEn},
}

// ifaceLangLabel 返回偏好在本帧语言下的显示名；不认识的偏好按跟随系统。
func ifaceLangLabel(pref string) string {
	for _, o := range ifaceLangs {
		if o.pref == pref {
			return o.label()
		}
	}
	return ifaceLangs[0].label()
}

// ifaceLangLabels 返回下拉在本帧语言下的全部选项（顺序同 ifaceLangs）。
func ifaceLangLabels() []string {
	out := make([]string, len(ifaceLangs))
	for i, o := range ifaceLangs {
		out[i] = o.label()
	}
	return out
}

// ifaceLangPref 把下拉里的显示名换回偏好值：认不出来（不在表里）时按
// 跟随系统，不猜——下拉只会给出表里的名字。
func ifaceLangPref(label string) string {
	for _, o := range ifaceLangs {
		if o.label() == label {
			return o.pref
		}
	}
	return ifaceLangs[0].pref
}

// langLabels 返回 Select 用的选项（可读名，顺序同 targetLangs）。
func langLabels() []string {
	out := make([]string, len(targetLangs))
	for i, l := range targetLangs {
		out[i] = l.label()
	}
	return out
}

// langLabelFor 返回规范语言码对应的显示名；不在列表里返回空串。
func langLabelFor(code string) string {
	for _, l := range targetLangs {
		if l.code == code {
			return l.label()
		}
	}
	return ""
}

// langCodeFor 返回显示名对应的规范码；不认识的显示名原样返回，让
// store.NormalizeTargetLang 去拒绝它（不在这里猜）。
func langCodeFor(label string) string {
	for _, l := range targetLangs {
		if l.label() == label {
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
		s.note = locales.UiSettingsUnknownTargetLang(langLabelFor(code))
	}
	s.lang = langLabelFor(code)
	if s.lang == "" {
		s.lang = targetLangs[0].label()
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
	a.status, a.statusErr = locales.UiStatusSavingSettings(), false

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
				a.status, a.statusErr = locales.UiStatusSettingsSaveFailed(err.Error()), true
				return
			}
			a.settingsMu.Lock()
			if a.settings.CreatedAt == "" {
				a.settings.CreatedAt = out.CreatedAt
			}
			a.settingsMu.Unlock()
			a.settingsErr, a.settingsLoaded = "", true
			if a.settings.Ready() {
				a.status, a.statusErr = locales.UiStatusSettingsSaved(), false
			} else {
				a.status, a.statusErr = locales.UiStatusSettingsSavedShort(), false
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
//	界面语言  [跟随系统 ▾]
//	（说明：密钥明文保存；译文只用于界面显示）
//	（提示 / 错误）
//	                         [取消] [保存]
//
// 「界面语言」是即时生效的偏好（选中即静默写 config.json，不经过「保存」）：
// 它与其他翻译设置不是一类东西，且用户期望选完立刻看到界面变样。
func (a *App) viewSettingsModal(c *myui.Context) {
	s := &a.settingsUI
	t := c.Theme()
	ifaceLabel := locales.UiSettingsInterfaceLang()
	myui.Modal(c, &s.open, func() {
		myui.Column(c).Width(520).Gap(t.Space(3)).Children(func() {
			myui.Column(c).Gap(t.Space(1)).Children(func() {
				myui.Text(c, locales.UiSettingsTitle()).FontSize(t.FontSize + 3).Bold()
				myui.Text(c, locales.UiSettingsTranslateHeading()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
			// settings.json 读不出来（JSON 损坏）：说清楚，保存会覆盖它。
			if a.settingsErr != "" {
				myui.Box(c).FillWidth().Padding(t.Space(1.5), t.Space(3)).Radius(t.Radius).
					Background(t.Warning.Alpha(0.10)).Border(1, t.Warning.Alpha(0.4)).Children(func() {
					myui.Text(c, locales.UiSettingsLoadFailed(a.settingsErr)).MaxLines(4).TextColor(t.Warning)
				})
			}
			myui.Form(c, func() {
				myui.Field(c, locales.UiSettingsBaseURL(), func() {
					myui.TextInput(c, &s.baseURL).Background(a.pal.canvas).FillWidth().
						Placeholder("https://api.openai.com/v1").Label(locales.UiSettingsBaseURL())
				})
				myui.Field(c, locales.UiSettingsApiKey(), func() {
					myui.TextInput(c, &s.apiKey).Background(a.pal.canvas).FillWidth().
						Placeholder("sk-…").Label(locales.UiSettingsApiKey()).Password()
				})
				myui.Field(c, locales.UiSettingsModel(), func() {
					myui.TextInput(c, &s.model).Background(a.pal.canvas).FillWidth().
						Placeholder("gpt-4o-mini").Label(locales.UiSettingsModel())
				})
				myui.Field(c, locales.UiSettingsTargetLang(), func() {
					myui.Select(c, &s.lang, langLabels()).Label(locales.UiSettingsTargetLang())
				})
				myui.Field(c, ifaceLabel, func() {
					// 下拉绑的是「本帧的显示名」而不是持久字段：语言一切，选项名
					// 就跟着变，把上一次语言的显示名留在状态里会显示成旧语言。
					// Changed 返回 true 的那次构建里 sel 已是新选中的显示名（同
					// viewThemeSwitch 的 Segmented），这才不会丢掉点击。
					sel := ifaceLangLabel(a.langPref)
					if myui.Select(c, &sel, ifaceLangLabels()).Label(ifaceLabel).Changed() {
						a.SetLangAsync(ifaceLangPref(sel))
					}
				})
			})
			myui.Text(c, locales.UiSettingsSecretNote()).
				FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			// 没填全不阻止保存（可能只想先选语言），只提醒翻译暂时用不了。
			// 它与下面的 s.note / s.err 都是**翻译那一组**的消息，所以要排在
			// 「更新」分隔线之前：落在分隔线之后就会读成在说自动检查更新。
			draft := s.settingsDraft()
			if !draft.Ready() {
				myui.Text(c, locales.UiSettingsIncomplete()).
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
			// 「更新」是另一类偏好（应用自更新，不是翻译），用一条分隔线独立成组：
			// 组内只有一项——「自动检查更新」。
			myui.Divider(c).Margin(t.Space(1), 0)
			myui.Column(c).Gap(t.Space(1.5)).Children(func() {
				myui.Text(c, locales.UiSettingsUpdateHeading()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
				myui.Row(c).FillWidth().AlignItems(myui.Center).Children(func() {
					myui.Text(c, locales.UiSettingsAutoCheckUpdate()).Grow(1).MinWidth(0)
					// 「自动检查更新」是即时生效的偏好（同界面语言）：切了就静默写盘，
					// 不经过「保存」，值存在 config.json 的 autoCheckUpdates（默认开）。
					// Changed 返回 true 的那次构建里 on 已是新值（同 viewCard 的
					// 开关），不要写成构建中途直接比较局部变量。
					on := a.autoCheck
					if kuraSwitch(c, &on, a.pal.toggle).Label(locales.UiSettingsAutoCheckA11y()).Changed() {
						a.SetAutoCheckAsync(on)
					}
				})
			})
			myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
				myui.Spacer(c)
				if myui.Button(c, locales.UiCommonCancel()).Disabled(s.busy).Clicked() {
					s.open = false
				}
				// 无障碍名称写成「确认保存」，与别处文字区分（同安装弹窗）。
				if myui.PrimaryButton(c, locales.UiCommonSave()).Label(locales.UiSettingsSaveA11y()).Disabled(s.busy).Clicked() {
					a.SaveSettingsAsync(s.settingsDraft())
				}
			})
		})
	})
}
