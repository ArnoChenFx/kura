package store

import (
	"io"
	"os"
	"path/filepath"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// copyDir 递归复制目录（保留普通文件与子目录）。dst 必须不存在或为空。
//
// 链接目录（Windows 联接点、目录符号链接）会被穿透复制成真目录：src 自己是链接、或树里含有
// 链接都行，结果里没有任何链接，内容是真实的。快照靠它——一个只是指向原目标的链接对
// 技能毫无保护意义。指向文件的符号链接照旧按普通文件读取内容。链接指回祖先目录造成的环会被跳过。
func copyDir(src, dst string) error { return copyTree(src, dst, false, false) }

// copyTree 同 copyDir。skipGit 为真时跳过根级 .git（clone 产物，更新时本来就
// 要删，跳过就不必把整个对象库复制一遍）。untrusted 为真表示 src 是 git clone 的产物：
// 仓库里的链接不可信，树里的链接目录不进入（见 walkOpts.noSubLinks），避免一个恶意仓库的
// 「链接到根目录」把整个磁盘拖进技能目录。
func copyTree(src, dst string, skipGit, untrusted bool) error {
	return walkTree(src, walkOpts{noSubLinks: untrusted}, func(e treeEntry) error {
		if skipGit && e.Rel == ".git" {
			if e.IsDir {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(e.Rel))
		if e.IsDir {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(e.Path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return i18n.Wrapf(err, "storeErrCopyFile", func() string { return locales.StoreErrCopyFile(e.Rel) })
		}
		return nil
	})
}
