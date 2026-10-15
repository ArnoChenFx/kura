package store

// sentinel.go — 「整句文案来自 locales、身份来自哨兵」的错误。
//
// 旧实现（internal/store/catalog.go 的 withSentinel）随自写 i18n 一起退役；
// 语义原样保留，实现搬到 internal/i18n.WithSentinel，store 只留一个薄别名，
// 免得每个调用点都写长名字。
//
// 用在「哨兵自带的文字不够、还要补上下文（文件名、位置）」而整句又要完整
// 进目录的场合，例如「store: 未找到该技能: x（回收站中不存在）」。

import "github.com/ArnoChenFx/kura/internal/i18n"

// sentinelErr 造一个身份是 base、文字来自 locales 的错误。
func sentinelErr(base error, key string, render ...func() string) error {
	return i18n.WithSentinel(base, key, render...)
}
