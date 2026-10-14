package store

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// HashDir 计算一个目录的内容哈希：sha256，输入为「相对路径 + 文件内容」的
// 拼接流，相对路径使用 / 分隔并按字节序排序，.kura-meta.json 自身排除。
// 空目录返回有意义的哈希（仍可稳定复现）。
// 返回值形如 "sha256:hex"，与 REQUIREMENTS.md 的 contentHash 格式一致。
func HashDir(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == MetaFileName {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", i18n.Wrapf(err, "store.err.walkDir", dir)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		io.WriteString(h, rel)
		h.Write([]byte{0})
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", i18n.Wrapf(err, "store.err.readFile", rel)
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
