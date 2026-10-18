package store

// autocheck.go — 应用自更新的两项状态，存在 Home/config.json 里：
//
//   - autoCheckUpdates：「自动检查更新」偏好，是唯一真值源。插件 updater.json
//     里的 automaticChecks 只是 kura 按它派生出来的调度开关（见 selfupdate）。
//   - updateCheckedAt：kura 自己最近一次成功检查应用更新的时间，决定下一次后台
//     检查何时到。插件的 lastCheck 只记插件自己的检查，kura 的检查不经过插件。

import (
	"time"
)

// ResolveAutoCheck 返回「自动检查更新」偏好的当前值。config.json 里已经有这一项时
// 直接用它；还没有（升级前的旧版本，或第一次启动）时，把 legacy（插件 updater.json
// 里旧版本留下的 automaticChecks，nil 表示从未设过）定下来写进 config.json，之后
// 不再看 legacy。只迁移这一次：每次启动 kura 都会把派生值写回插件，要是每次都从
// 插件文件读，派生值就会被当成用户的选择。
//
// config.json 损坏时返回缺省（开）与错误，且不写盘，不覆盖用户手改的内容。
func ResolveAutoCheck(c *Config, legacy *bool) (bool, error) {
	configMu.Lock()
	defer configMu.Unlock()
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		return true, err
	}
	if hc.AutoCheck != nil {
		return *hc.AutoCheck, nil
	}
	on := true
	if legacy != nil {
		on = *legacy
	}
	hc.AutoCheck = &on
	markCreated(hc)
	return on, SaveHomeConfig(c, hc)
}

// SetAutoCheck 把「自动检查更新」偏好写进 config.json（其余内容原样保留），并更新
// 内存里的值。config.json 损坏时整个覆盖（见 fallbackHome）：这是用户刚做出的修改，
// 与 SetTheme 的处理一致。
func (l *Library) SetAutoCheck(on bool) error {
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		hc = l.fallbackHome()
	}
	markCreated(hc)
	hc.AutoCheck = &on
	if err := SaveHomeConfig(c, hc); err != nil {
		return err
	}
	l.AutoCheck = on
	return nil
}

// LastUpdateCheck 返回 kura 最近一次成功检查应用更新的时间；从未检查过、或文件里
// 没有这一项、或时间格式不认识时返回零值。config.json 损坏时返回错误。
func LastUpdateCheck(c *Config) (time.Time, error) {
	configMu.Lock()
	defer configMu.Unlock()
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, hc.UpdateCheckedAt)
	if err != nil {
		return time.Time{}, nil
	}
	return t, nil
}

// SetLastUpdateCheck 记下 kura 最近一次成功检查应用更新的时间（其余内容原样保留）。
// config.json 损坏时返回错误且不写盘，与 DiscoverAgentRoots 一致：后台写入不覆盖
// 用户手改的内容。
func SetLastUpdateCheck(c *Config, t time.Time) error {
	configMu.Lock()
	defer configMu.Unlock()
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		return err
	}
	markCreated(hc)
	hc.UpdateCheckedAt = t.UTC().Format(time.RFC3339)
	return SaveHomeConfig(c, hc)
}

// markCreated 在配置还没有创建时间时补上，与其他写入 config.json 的路径一致。
func markCreated(hc *HomeConfig) {
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
}
