package store

// 文档读取先限制字节数，避免异常大的 SKILL.md 占满内存或整份发送给翻译服务。

import (
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ReadSkillDocument 返回最多 limit 字节的源码；truncated 为真时界面应提示截断。
// limit 必须为正。源码截断停在 UTF-8 边界，原文件始终不修改。
func ReadSkillDocument(dir string, limit int) (src string, truncated bool, err error) {
	if limit <= 0 {
		return "", false, os.ErrInvalid
	}
	f, err := os.Open(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) <= limit {
		return string(data), false, nil
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return string(data[:cut]), true, nil
}
