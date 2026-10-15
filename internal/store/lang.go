package store

// lang.go — 界面语言偏好（config.json 的 lang 字段）与它的读写。
//
// 取值与 i18n 的 Lang 一一对应，但持久化用的字符串只有三种：system（跟随
// 系统）、zh、en。解析交给 i18n.Normalize（一处实现，命令行与应用窗口共用
// 同一套判定），这里只做「存储值 ↔ i18n.Lang」的映射。

import (
	"fmt"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// 界面语言的存储取值（config.json 的 lang）。
const (
	// LangSystem 跟随系统的界面语言：中文系统用中文，其余用英文。默认值。
	LangSystem = "system"
	// LangZH 固定中文。
	LangZH = "zh"
	// LangEN 固定英文。
	LangEN = "en"
)

// ErrUnknownUILang 表示语言偏好不是 system、zh 或 en。
var ErrUnknownUILang = i18n.NewError("storeErrUnknownUILang", func() string { return locales.StoreErrUnknownUILang() })

// NormalizeLang 把配置里的语言值规范成三个存储取值之一：空串（与
// "system"、"auto"）是跟随系统；zh 与 en 的大小写变体、以及 zh-CN、en-US
// 这类标签都归到对应的语言；不认识的值返回 ErrUnknownUILang。
func NormalizeLang(v string) (string, error) {
	l, ok := i18n.Normalize(v)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownUILang, v)
	}
	switch l {
	case i18n.LangSystem:
		return LangSystem, nil
	case i18n.ZH:
		return LangZH, nil
	default:
		return LangEN, nil
	}
}

// LangPref 返回当前语言偏好对应的 i18n.Lang：system 时是 i18n.LangSystem
// （不是一种语言，要用 i18n.Resolve 配合系统 locale 解成具体语言）。
//
// 配置里的值理论上已经规范过，认不出时按跟随系统处理（与主题、排列模式
// 的容错一致）。
func LangPref(stored string) i18n.Lang {
	l, ok := i18n.Normalize(stored)
	if !ok {
		return i18n.LangSystem
	}
	return l
}

// SetLang 保存界面语言偏好，并更新内存里的值（SetTheme / SetView 的同型
// 实现：先在调用方校验，再串行读—改—写 config.json）。
func (l *Library) SetLang(v string) error {
	stored, err := NormalizeLang(v)
	if err != nil {
		return err
	}
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		hc = &HomeConfig{Roots: append([]string(nil), l.Roots...)}
		if l.Theme != "" && l.Theme != ThemeSystem {
			hc.Theme = l.Theme
		}
		if l.View != "" && l.View != ViewGrid {
			hc.View = l.View
		}
	}
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hc.Lang = stored
	if err := SaveHomeConfig(c, hc); err != nil {
		return err
	}
	l.Lang = stored
	return nil
}

// langFromHome 从配置里取出语言偏好：缺失、空串或不认识的值都算跟随系统
// （与主题、排列模式的容错一致，不当作错误）。
func langFromHome(hc *HomeConfig) string {
	if stored, err := NormalizeLang(strings.TrimSpace(hc.Lang)); err == nil {
		return stored
	}
	return LangSystem
}
