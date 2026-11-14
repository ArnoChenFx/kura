package store

// 链接目录形式的技能：Windows 联接点（junction）与目录符号链接。
//
// CherryStudio 等程序会把技能目录以联接点的形式建到 ~/.agents/skills 下，真实内容在别处。
// 这类条目必须当成技能：扫描、计数、doctor、哈希与复制、快照、移动、删除、更新都要对。
// 本文件是这些行为共用的判定与解析，其余文件不再各写一份 e.IsDir()。
//
// 先弄清 Go 在 Windows 上对它们的报告（实测，Go 1.27）：
//
//   - 联接点的 Lstat / DirEntry.Type() 是 ModeIrregular：既不是 ModeDir 也不是 ModeSymlink，
//     所以 e.IsDir() 为 false；os.Stat 会跟随它，得到目标的信息（IsDir 为 true）。
//   - 目录符号链接是 ModeSymlink，os.Stat 同样跟随。
//   - filepath.WalkDir 不会进入联接点（根的 Lstat 就是 irregular，d.IsDir() 为 false），
//     所以 HashDir / copyDir 在链接目录上读不出任何内容（见 walk.go 的 walkTree）。
//   - filepath.EvalSymlinks 对联接点无效：返回它自己，不能依赖它来找真实目录；
//     os.Readlink 对联接点可用，返回目标的绝对路径。
//   - os.Rename / os.Remove / os.RemoveAll 作用在链接本身：移动、删除的都是链接，不穿透到目标。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// maxLinkHops 是解析一串链接（链接指向链接）时最多跟随的跳数，超过就当作成环、解析失败。
// 与系统自己的上限同量级（Linux 40，Windows 63），取得小一点，足够任何真实布局。
const maxLinkHops = 32

// isLinkMode 报告类型位 m（Lstat 或 DirEntry.Type() 的结果）是不是「链接」：符号链接，或者
// Windows 的联接点——Go 把联接点和其他不认识的重解析点统一报成 ModeIrregular。
func isLinkMode(m os.FileMode) bool { return m&(os.ModeSymlink|os.ModeIrregular) != 0 }

// isSkillDirEntry 判断目录 dir 下的条目 e 是不是一个技能目录，是整个 store 唯一的判定口径：
//
//  1. 普通目录（e.IsDir()）；
//  2. 符号链接，且 os.Stat 成功、目标是目录（目录符号链接）；
//  3. Windows 联接点等既不是 ModeDir 也不是 ModeSymlink 的链接（ModeIrregular），且 os.Stat 成功、
//     目标是目录。
//
// os.Stat 失败（断链、目标不存在、无权限、成环）一律不算：按「目录不存在」处理，不显示。
// 普通文件与设备等不是链接的条目直接返回 false，不做多余的 Stat。
func isSkillDirEntry(dir string, e os.DirEntry) bool {
	t := e.Type()
	if t.IsDir() {
		return true
	}
	if !isLinkMode(t) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// IsSkillDirEntry 是 isSkillDirEntry 的导出版本：internal/cli 的 doctor 需要同一份判定，
// 判定口径只能有这一处（见 isSkillDirEntry 的说明）。
func IsSkillDirEntry(dir string, e os.DirEntry) bool { return isSkillDirEntry(dir, e) }

// IsBrokenLinkEntry 报告 dir 下的条目 e 是不是断链：它是符号链接或联接点，但目标不存在、
// 不可访问或成环。断链按「目录不存在」处理——既不是技能，也不能说成「同名普通文件」。
func IsBrokenLinkEntry(dir string, e os.DirEntry) bool {
	if !isLinkMode(e.Type()) {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, e.Name()))
	return err != nil
}

// realDir 把路径 path 解析成真正存放内容的目录。
//
//   - path 本身不是链接：原样返回，ok 取决于它是不是目录；
//   - path 是符号链接或联接点：用 os.Readlink 取目标，相对目标按链接所在目录换算成绝对路径，
//     再接着解析（链接指向链接也行，最多 maxLinkHops 跳）。
//
// 不依赖 filepath.EvalSymlinks：它对联接点无效。ok 为 false 表示解析不出一个存在的目录
// （断链、目标是文件、成环）。读不出目标的重解析点（比如云盘的占位目录）按它自己的路径用，
// 是不是目录看 os.Stat。
func realDir(path string) (string, bool) {
	cur := path
	for hops := 0; hops <= maxLinkHops; hops++ {
		fi, err := os.Lstat(cur)
		if err != nil {
			return path, false
		}
		if !isLinkMode(fi.Mode()) {
			return cur, fi.IsDir()
		}
		target, err := os.Readlink(cur)
		if err != nil {
			st, serr := os.Stat(cur)
			return cur, serr == nil && st.IsDir()
		}
		cur = filepath.Clean(absLinkTarget(cur, target))
	}
	return path, false
}

// absLinkTarget 把链接 link 的目标 target 换成绝对路径：相对目标按链接所在目录算，Windows 上
// 不带盘符的「根相对」目标（\foo\bar）借用链接所在的盘符。
func absLinkTarget(link, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	if len(target) > 0 && os.IsPathSeparator(target[0]) {
		return filepath.VolumeName(link) + target
	}
	return filepath.Join(filepath.Dir(link), target)
}

// canonicalDir 把 p 规范成判重用的绝对路径：解析其中的符号链接祖先、取磁盘上的真实大小写；
// 解析不了（目录不存在等）时退回 Clean 之后的绝对路径。联接点祖先 EvalSymlinks 解析不了，
// 保持原样——这只影响判重的精度，不影响正确性（成环最终都会被 walkTree 的判重或系统的
// 链接层数上限拦住）。比较时配合 rootKey 抹掉 Windows 的大小写差异。
func canonicalDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

// removeEntry 删除路径 p 本身。p 是链接（符号链接或联接点）时只摘掉链接，**绝不**进入它指向的
// 目录；否则按目录树整个删除。os.RemoveAll 在各平台上本来就不会穿透链接（实测），这里仍先
// Lstat 一次，让「只删链接」成为代码里读得到的保证，而不是依赖标准库的实现细节。
// 路径本来就不存在不算错误。
func removeEntry(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if isLinkMode(fi.Mode()) {
		// 能读出目标的才是真链接，只摘链接；读不出目标的重解析点（云盘占位目录之类）
		// 按普通目录处理，否则 os.Remove 会因为「目录非空」而删不掉。
		if _, rerr := os.Readlink(p); rerr == nil {
			return os.Remove(p)
		}
	}
	return os.RemoveAll(p)
}

// pathContains 报告 child 是否就是 parent 或在 parent 之内：两边先规范成绝对路径（解析符号链接
// 祖先），再按 rootKey 比较（Windows 忽略大小写）。
func pathContains(parent, child string) bool {
	p, c := rootKey(canonicalDir(parent)), rootKey(canonicalDir(child))
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// checkReplaceTarget 在对「链接指向的真实目录」target 做替换（先 RemoveAll 再放入新内容）之前把关。
// 链接可以指到任何地方，而替换会先删光目标，所以拒绝两类会连带毁掉别的东西的目标：
//
//   - 文件系统根（盘符根、/）；
//   - 把 kura 自己的技能根目录或数据目录包含在内的目录（链接指回了上级目录，甚至指回根目录本身）。
//
// 普通（非链接）技能目录不走这里：它一定在根目录之内，行为与以前完全一致。
func (c *Config) checkReplaceTarget(name, target string) error {
	abs := canonicalDir(target)
	if parent := filepath.Dir(abs); parent == abs || pathContains(abs, c.Root) || pathContains(abs, c.Home) {
		return i18n.NewError("storeErrReplaceTargetUnsafe", func() string { return locales.StoreErrReplaceTargetUnsafe(name, abs) })
	}
	return nil
}

// DirExists 报告 path 当前是否是一个存在的目录。os.Stat 会跟随链接，所以指向存在目录的联接点、
// 目录符号链接也算；断链、目标不是目录、无权限都算不存在。界面用它判断一个技能目录要不要出现在
// 左栏，口径与 isSkillDirEntry 一致（os.Stat 成功且是目录）。
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
