package store

import (
	"io"
	"os"
	"path/filepath"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// copyDir 递归复制目录（保留普通文件与子目录；不跟随符号链接，遇符号链接
// 按普通文件读取内容）。dst 必须不存在或为空。
func copyDir(src, dst string) error { return copyTree(src, dst, false) }

// copyTree 同 copyDir；skipGit 为真时跳过根级 .git（clone 产物，更新时本来就
// 要删，跳过就不必把整个对象库复制一遍）。
func copyTree(src, dst string, skipGit bool) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if skipGit && rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
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
			return i18n.Wrapf(err, "storeErrCopyFile", func() string { return locales.StoreErrCopyFile(rel) })
		}
		return nil
	})
}
