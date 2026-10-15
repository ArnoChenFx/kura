package i18n

// error.go — 目录错误的薄层。
//
// 为什么不让调用点直接取 locales.Xxx() 再 fmt.Errorf：那样文字在**构造时**
// 就被定死了，后台任务里产生的错误在用户切换语言之后才显示，就会显示成旧
// 语言的句子；而且 errors.Is 的身份也没了。
//
// 这一层把两件事分开：
//
//   - 身份：由 key 决定（Error.Is 比较 key），与语言无关，可作哨兵；
//   - 文字：每次调用 Error() 时才由 render 现取（render 内部调
//     locales.Xxx()，读的是当时的语言）。
//
// 迁移前 i18n.NewError / Errorf / Wrapf 的语义完全保留，只是「按 key 取文字」
// 那一步换成了调用点传入的 locales 函数。

import (
	"github.com/ArnoChenFx/kura/locales"
	myi18n "github.com/egoist/mygo/i18n"
)

// catalogText 按 key 取当前语言的无参数文案：只在 NewError 没给 render 时
// 兜底（调用点一律传 locales 函数，这样参数个数错误在编译期就暴露）。
func catalogText(key string) string {
	return locales.Catalog.Format(myi18n.Locale(), key, nil)
}

// Error 是一个文案来自 locales 的错误：身份稳定（同一个 key 相等），文字
// 随当前语言变化。
type Error struct {
	key    string
	render func() string
}

// NewError 造一个由 key 决定身份、由 render 决定文字的错误。render 省略时
// 按 key 现取（无参数的整句）；有参数时传一个闭包，例如：
//
//	i18n.NewError("storeErrInstallSkill", func() string {
//		return locales.StoreErrInstallSkill(name)
//	})
//
// render 在每次 Error() 时调用，所以文字跟着语言变。
func NewError(key string, render ...func() string) *Error {
	var r func() string
	if len(render) > 0 {
		r = render[0]
	}
	return &Error{key: key, render: r}
}

// Error 实现 error：每次调用都按当前语言取文案。
func (e *Error) Error() string {
	if e.render != nil {
		return e.render()
	}
	return catalogText(e.key)
}

// Key 返回这条错误对应的目录 key（新式扁平 key，如 storeErrUnknownSkill）。
func (e *Error) Key() string { return e.key }

// Is 让同一个 key 的错误彼此相等（参数不同也算同一类错误）。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.key == e.key
}

// Errorf 与 NewError 同形，便于表达「这里要的是一个 error」。
func Errorf(key string, render ...func() string) error { return NewError(key, render...) }

// Wrapf 在整句文案末尾接上 ": " 包住 err，例如
// "store: 安装 x 失败: <err>"。文字在调用 Error() 时才生成（错误可能在
// 后台任务里产生、在切换语言之后才显示），身份来自被包住的错误。
func Wrapf(err error, key string, render ...func() string) error {
	return &wrapped{err: err, msg: NewError(key, render...)}
}

// wrapped 是 Wrapf 的结果。
type wrapped struct {
	err error
	msg *Error
}

// Error 实现 error：每次调用都按当前语言取文案。
func (w *wrapped) Error() string { return w.msg.Error() + ": " + w.err.Error() }

// Unwrap 让 errors.Is / errors.As 能穿到底层错误。
func (w *wrapped) Unwrap() error { return w.err }

// WithSentinel 造一个「整句文案来自目录、身份来自哨兵 base」的错误：
// 文字随语言变，errors.Is(err, base) 照样成立。
//
// 用在「哨兵自带的文字不够、还要补上下文（文件名、位置）」而整句又要完整
// 进目录的场合。
func WithSentinel(base error, key string, render ...func() string) error {
	return &withSentinel{base: base, msg: NewError(key, render...)}
}

// withSentinel 见 WithSentinel。
type withSentinel struct {
	base error
	msg  *Error
}

// Error 实现 error：每次渲染都按当前语言取文案。
func (e *withSentinel) Error() string { return e.msg.Error() }

// Unwrap 让 errors.Is / errors.As 穿透到哨兵（身份不受语言影响）。
func (e *withSentinel) Unwrap() error { return e.base }
