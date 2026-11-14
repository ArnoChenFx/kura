package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// HashDir 计算一个目录的内容哈希：sha256，输入为「相对路径 + 文件内容」的
// 拼接流，相对路径使用 / 分隔并按字节序排序，.kura-meta.json 自身排除。
// 空目录返回有意义的哈希（仍可稳定复现）。
// 返回值形如 "sha256:hex"，与 REQUIREMENTS.md 的 contentHash 格式一致。
//
// dir 本身可以是链接目录（Windows 联接点、目录符号链接），树里也可以包含链接目录：
// 遍历用 walkTree（会进入它们、防环），哈希结果与「把链接穿透复制成真目录」后的
// 哈希一致。指向文件的符号链接照旧按文件读内容。
func HashDir(dir string) (string, error) {
	return hashDirContext(context.Background(), dir)
}

// hashDirContext 让只读检查能在目录项与文件之间响应取消。
func hashDirContext(ctx context.Context, dir string) (string, error) {
	var paths []string
	err := walkTree(dir, walkOpts{}, func(e treeEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir || e.Rel == MetaFileName {
			return nil
		}
		paths = append(paths, e.Rel)
		return nil
	})
	if err != nil {
		return "", i18n.Wrapf(err, "storeErrWalkDir", func() string { return locales.StoreErrWalkDir(dir) })
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		io.WriteString(h, rel)
		h.Write([]byte{0})
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", i18n.Wrapf(err, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
