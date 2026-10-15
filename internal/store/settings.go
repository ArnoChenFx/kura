package store

// settings.go — 软件设置的读写（需求第 6 条的后端部分）。
//
// 设置明文存放在 <Home>/settings.json（与 config.json 同级）。**明文是用户
// 明确拍板的方案**：kura 是本地单人使用的桌面应用，不做加密、不接 Windows
// 凭据管理器。代价是任何能读该用户目录的进程都能拿到 API key，因此文件
// 权限收紧到 0o600（比 config.json 的 0o644 更严），且 API key 绝不出现在
// 日志、错误信息或任何写回磁盘的内容里。
//
// 边界：本文件不 import 任何 UI/CLI 包；不在 Config 上新增方法（config.go
// 是既有文件，由其它约定维护），路径都从 c.Home 推导。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// SettingsFileName 是 Home 下设置文件的文件名，与 config.json 同级。
const SettingsFileName = "settings.json"

// settings 相关的哨兵错误：调用方可以用 errors.Is 精确分支，而不是匹配错误文本。
var (
	// ErrUnknownLang 表示目标语言代码不在支持列表里。
	ErrUnknownLang = i18n.NewError("storeErrUnknownLang", func() string { return locales.StoreErrUnknownLang() })
	// ErrNotConfigured 表示翻译所需的 BaseURL/APIKey/Model 尚未填齐。
	// （放进哨兵列表是因为 UI 只需要「弹设置弹窗」这一个分支，不需要看
	// 具体缺哪项；缺哪项由错误文本说明。）
	ErrNotConfigured = i18n.NewError("storeErrNotConfigured", func() string { return locales.StoreErrNotConfigured() })
	// ErrNoSettings 表示 Home 下还没有 settings.json（用户从未保存过设置）。
	// 与「文件存在但内容零值」区分开：前者通常意味着应该展示空的设置表单，
	// 后者意味着应该展示已保存的（可能不完整的）值。LoadSettings 不返回它
	// （文件不存在返回 false 而不是错误）；它是给 UI 在磁盘上探测文件是否存在
	// 之外、需要明确语义时用的哨兵。
	ErrNoSettings = i18n.NewError("storeErrNoSettings", func() string { return locales.StoreErrNoSettings() })
)

// DefaultTargetLang 是 TargetLang 为空串时的默认值（简体中文）。
const DefaultTargetLang = "zh-CN"

// Settings 是 ~/.kura/settings.json 的内容：OpenAI 兼容翻译 API 的接入信息
// 与全局翻译目标语言。JSON 字段用小写驼峰、缩进两格，便于手工编辑，
// 风格照 config.go 的 HomeConfig。
type Settings struct {
	// BaseURL 是 OpenAI 兼容 API 的根地址（如 https://api.openai.com/v1）。
	// 允许指向任何兼容 /chat/completions 的服务（如本地推理网关）。
	BaseURL string `json:"baseUrl"`
	// APIKey 是 API 密钥。**明文存储**——这是用户明确选择的本地单人方案，
	// 不接 Windows 凭据管理器、不做加密；文件权限 0o600 是唯一的兜底。
	// 绝不能把它写进日志、错误信息或任何导出文件。
	APIKey string `json:"apiKey"`
	// Model 是模型名（如 gpt-4o-mini）。
	Model string `json:"model"`
	// TargetLang 是全局翻译目标语言（如 zh-CN、en），规范形式见
	// NormalizeTargetLang。空串按 DefaultTargetLang 处理。
	TargetLang string `json:"targetLang,omitempty"`
	// CreatedAt 是设置首次保存的时间（RFC3339），之后不再改动。
	CreatedAt string `json:"createdAt,omitempty"`
}

// langCanonical 是规范化表：键是允许的输入形式（统一小写、分隔符统一成
// “-”后比较），值是唯一的规范输出。新增语言在这里加一行即可。
// 用显式键值映射而不是 locale 库：支持哪些语言是产品决策，列出来更清楚。
var langCanonical = map[string]string{
	"zh":      "zh-CN",
	"zh-cn":   "zh-CN",
	"zh_cn":   "zh-CN",
	"zh-hans": "zh-CN",
	"en":      "en",
	"en-us":   "en-US",
	"en-gb":   "en-GB",
	"ja":      "ja",
	"ko":      "ko",
	"fr":      "fr",
	"de":      "de",
	"es":      "es",
	"pt":      "pt",
	"ru":      "ru",
}

// langLabel 是 LangLabel 用的可读名字表（键是规范语言码）：值是 locales 的
// 取值函数。
//
// 这些名字是语言的**自称**（glossonym）：简体中文在中文界面与英文界面下
// 都写作「简体中文」，English 在两种界面下也都写作 English，所以两种语言的
// 目录取值相同。它们仍然进目录（而不是留在代码里）只有一个原因：
// 「界面文案不硬编码」这条守卫只能有一个标准（见 internal/i18n 的
// TestNoHardcodedCJK）。
var langLabel = map[string]func() string{
	"en":    locales.StoreLangNameEn,
	"zh-CN": locales.StoreLangNameZhCN,
	"en-US": locales.StoreLangNameEnUS,
	"en-GB": locales.StoreLangNameEnGB,
	"ja":    locales.StoreLangNameJa,
	"ko":    locales.StoreLangNameKo,
	"fr":    locales.StoreLangNameFr,
	"de":    locales.StoreLangNameDe,
	"es":    locales.StoreLangNameEs,
	"pt":    locales.StoreLangNamePt,
	"ru":    locales.StoreLangNameRu,
}

// settingsPath 返回 Home/settings.json 的路径。路径拼接一律 filepath.Join。
func settingsPath(c *Config) string { return filepath.Join(c.Home, SettingsFileName) }

// LoadSettings 从 <Home>/settings.json 读取设置。第二个返回值为 false 表示
// 文件不存在（返回零值结构、**不报错**，与 LoadHomeConfig 的语义一致）。
//
// JSON 损坏时**报错**而不是静默按零值处理：settings.json 里有 API key，
// 静默丢掉等于让用户重填密钥；用户手改出坏 JSON 是可修复的用户问题，
// 把错误显式报给界面、由用户决定覆盖与否，比替他抹掉更安全。
func LoadSettings(c *Config) (*Settings, bool, error) {
	data, err := os.ReadFile(settingsPath(c))
	if errors.Is(err, os.ErrNotExist) {
		return &Settings{}, false, nil
	}
	if err != nil {
		return nil, false, i18n.Wrapf(err, "storeErrReadFile", func() string { return locales.StoreErrReadFile(settingsPath(c)) })
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false, i18n.Wrapf(err, "storeErrParseJSON", func() string { return locales.StoreErrParseJSON(settingsPath(c)) })
	}
	return &s, true, nil
}

// SaveSettings 把 s 写入 <Home>/settings.json。
//   - 权限 0o600：里面有 API key，比 config.json 的 0o644 更严（Windows 的
//     NTFS 不完全遵循 POSIX 权限位，但在支持它的文件系统上生效）。
//   - CreatedAt 为空时补当前时间（首次保存）；已有值不改动（首建时间不漂移）。
//   - 写盘前 ensureDirs() 保证 Home 存在，与 SaveHomeConfig 同样的写法。
//
// 并发：settings.json 目前只有一个写入口（设置弹窗保存），且不与 config.json
// 共享 configMu 的「读—改—写」竞争面，因此暂不加锁；若将来出现第二个写
// settings.json 的并发路径，必须先引入锁再改。
func SaveSettings(c *Config, s *Settings) error {
	if s == nil {
		return i18n.NewError("storeErrEmptySettings", func() string { return locales.StoreErrEmptySettings() })
	}
	if err := c.ensureDirs(); err != nil {
		return err
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return i18n.Wrapf(err, "storeErrMarshalSettings", func() string { return locales.StoreErrMarshalSettings() })
	}
	// 权限 0o600：含 API key 的文件，比 config.json（0o644）更严。
	if err := os.WriteFile(settingsPath(c), data, 0o600); err != nil {
		return i18n.Wrapf(err, "storeErrWriteFile", func() string { return locales.StoreErrWriteFile(settingsPath(c)) })
	}
	return nil
}

// NormalizeTargetLang 把用户输入的语言代码规范成受支持列表里的唯一形式：
//   - 大小写不敏感（zh-cn == zh-CN），分隔符 - 与 _ 等价（zh_CN == zh-CN）；
//   - 别名归并：zh / zh-cn / zh_CN / zh-Hans 都得到 zh-CN；
//   - 空串（或全是空白）返回 DefaultTargetLang；
//   - 不认识的值返回 ErrUnknownLang（**不猜**：把疑似拼写错误显式暴露出来，
//     比悄悄翻译成错误语言好）。
func NormalizeTargetLang(lang string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(lang))
	key = strings.ReplaceAll(key, "_", "-")
	if key == "" {
		return DefaultTargetLang, nil
	}
	if got, ok := langCanonical[key]; ok {
		return got, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownLang, lang)
}

// Ready 报告翻译所需的三项配置（BaseURL、APIKey、Model）是否都已填好。
// 判断标准是「去掉首尾空白后非空」：用户只打了几个空格不算配置完成。
// UI 用它决定「翻译」按钮是否可用；Translate 也用它拦截未配置的调用。
func (s *Settings) Ready() bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.BaseURL) != "" &&
		strings.TrimSpace(s.APIKey) != "" &&
		strings.TrimSpace(s.Model) != ""
}

// LangLabel 返回目标语言的可读名字（如 zh-CN → 简体中文），给界面显示用。
// 名字是语言的**自称**：两种界面语言下都一样（见 langLabel 的注释）。
// 空串与未登记的代码原样返回（未登记不是错误：NormalizeTargetLang 已经
// 把输入拦在受支持列表里，走到这里的未知值只可能来自手改的设置文件）。
func (s *Settings) LangLabel() string {
	lang := strings.TrimSpace(s.TargetLang)
	if lang == "" {
		lang = DefaultTargetLang
	}
	if label, ok := langLabel[lang]; ok {
		return label()
	}
	return lang
}
