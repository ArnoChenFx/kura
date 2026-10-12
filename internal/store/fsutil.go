package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyDir 递归复制目录（保留普通文件与子目录；不跟随符号链接，遇符号链接
// 按普通文件读取内容）。dst 必须不存在或为空。
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
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
			return fmt.Errorf("store: 复制 %s 失败: %w", rel, err)
		}
		return nil
	})
}
