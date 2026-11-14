package store

// walkTree：能进入链接目录（Windows 联接点、目录符号链接）的目录树遍历。
//
// filepath.WalkDir 在 Windows 上不会进入联接点，所以 HashDir、copyDir、预览、历史快照
// 这些要「读出一个技能目录里所有文件」的地方，遇到链接形式的技能就什么也读不到（背景见
// links.go）。walkTree 是它们共用的替代：用 os.ReadDir 递归，遍历根自己和树里的链接目录
// 都当目录进入。
//
// 与 filepath.WalkDir 刻意保持一致的约定，调用方可以几乎原样迁移：
//   - 目录项按文件名排序（os.ReadDir 的顺序），先回调目录自己再回调它的内容；
//   - 回调对目录返回 filepath.SkipDir 即跳过它的内容；对文件返回 filepath.SkipDir 则跳过
//     当前目录的其余项；返回 fs.SkipAll 终止整个遍历；
//   - 指向文件的符号链接保持原有语义：作为文件项交给调用方，按文件读内容，不会被当成目录。
//
// 防环：链接可以指回自己的祖先（比如 loop -> ..）。walkTree 沿路径记下每一层目录的真实路径
// （规范化大小写），遇到真实路径已在当前路径上的目录就直接跳过，不回调也不进入。它不会
// 死循环，也不会爆栈。跳过的是「环」而不是「重复」：两个链接指向同一个目录（菱形）不是环，
// 两处都会遍历，这样复制出来的快照才完整。
//
// 读写文件一律用沿原路径拼出来的 Path（经过链接也有效）而不是真实路径：这样即使判环的
// 规范化有漏网之鱼，系统自己的链接层数上限（Linux 40、Windows 63）也会让无限下钻以读目录
// 失败收场，而不是无限递归。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// treeEntry 是 walkTree 交给回调的一项。
type treeEntry struct {
	// Rel 是相对遍历根的路径，用 / 分隔；遍历根自己是 "."。
	Rel string
	// Path 是可以直接读写的路径：遍历根拼上 Rel，经过链接目录同样有效。
	Path string
	// IsDir 为真表示它是目录（包括被进入的链接目录）；为假的是文件类条目：普通文件、
	// 指向文件的符号链接、断链、设备等。
	IsDir bool
	// Mode 是条目自身（Lstat 口径）的类型位，调用方用它区分普通文件、链接与特殊文件。
	Mode os.FileMode
}

// walkOpts 调整 walkTree 的行为；零值是「进入遍历根与树里的全部链接目录」。
type walkOpts struct {
	// noSubLinks 为真时只解析遍历根自己，树里遇到的链接目录当文件类条目对待、不进入。
	// 用于 git clone 的产物：仓库里的符号链接不可信，进入它们可能把仓库之外的任意目录
	// （甚至整个磁盘）拖进技能目录；不进入时它们保持旧行为（读取时报错，操作失败）。
	noSubLinks bool
}

// treeWalker 保存一次遍历的状态。
type treeWalker struct {
	opt walkOpts
	fn  func(treeEntry) error
	// onPath 是当前路径上各层目录的真实路径键，用来发现「链接指回祖先」的环。
	onPath map[string]struct{}
}

// walkTree 遍历 root 下的整棵树，对每一项调用 fn。root 本身是链接时先解析到真实目录再进入；
// root 不是目录（普通文件、断链）时只回调 root 一次，像 filepath.WalkDir 对文件根那样把
// 后续处理（通常是读取时报错）交给调用方。root 不存在或读目录失败时返回错误。
func walkTree(root string, opt walkOpts, fn func(treeEntry) error) error {
	fi, err := os.Lstat(root)
	if err != nil {
		return err
	}
	realRoot, isDir := root, fi.IsDir()
	if isLinkMode(fi.Mode()) {
		realRoot, isDir = realDir(root)
	}
	if !isDir {
		return skipToNil(fn(treeEntry{Rel: ".", Path: root, Mode: fi.Mode()}))
	}
	if err := fn(treeEntry{Rel: ".", Path: root, IsDir: true, Mode: fi.Mode()}); err != nil {
		return skipToNil(err)
	}
	w := &treeWalker{opt: opt, fn: fn, onPath: map[string]struct{}{}}
	realRoot = canonicalDir(realRoot)
	w.onPath[rootKey(realRoot)] = struct{}{}
	return skipToNil(w.dir(root, ".", realRoot))
}

// skipToNil 把「跳过」类的哨兵当成正常结束。
func skipToNil(err error) error {
	if errors.Is(err, filepath.SkipDir) || errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}

// dir 遍历目录 path（rel 是它相对遍历根的路径，realPath 是它的规范真实路径）的内容。
func (w *treeWalker) dir(path, rel, realPath string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		childRel := name
		if rel != "." {
			childRel = rel + "/" + name
		}
		childPath := filepath.Join(path, name)
		childReal := filepath.Join(realPath, name)
		mode := e.Type()
		isDir := mode.IsDir()
		if !isDir && isLinkMode(mode) && !w.opt.noSubLinks {
			// 链接：指向目录的当目录进入，并换成它真实目录的路径来判环；
			// 指向文件的、断链的当文件类条目交给调用方。
			if target, ok := realDir(childReal); ok {
				isDir, childReal = true, canonicalDir(target)
			}
		}
		entry := treeEntry{Rel: childRel, Path: childPath, IsDir: isDir, Mode: mode}
		if !isDir {
			if err := w.fn(entry); err != nil {
				if errors.Is(err, filepath.SkipDir) {
					return nil // 对文件返回 SkipDir：跳过当前目录的其余项
				}
				return err
			}
			continue
		}
		key := rootKey(childReal)
		if _, cycle := w.onPath[key]; cycle {
			continue // 链接指回了路径上的祖先：环，不回调也不进入
		}
		if err := w.fn(entry); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				continue
			}
			return err
		}
		w.onPath[key] = struct{}{}
		err := w.dir(childPath, childRel, childReal)
		delete(w.onPath, key)
		if err != nil {
			return err
		}
	}
	return nil
}
