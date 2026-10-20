package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadSkillDocumentBoundsAndPreservesFile(t *testing.T) {
	dir := t.TempDir()
	src := "abcd文档" + strings.Repeat("x", 100)
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, truncated, err := ReadSkillDocument(dir, 6)
	if err != nil || !truncated || got != "abcd" || !utf8.ValidString(got) {
		t.Fatalf("读取边界错误：%q %v %v", got, truncated, err)
	}
	all, truncated, err := ReadSkillDocument(dir, len(src))
	if err != nil || truncated || all != src {
		t.Fatalf("长度恰好达到限制不应截断：%q %v %v", all, truncated, err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != src {
		t.Fatalf("原文被改变：%v", err)
	}
}

func TestReadSkillDocumentMissing(t *testing.T) {
	if _, _, err := ReadSkillDocument(t.TempDir(), 100); !os.IsNotExist(err) {
		t.Fatalf("缺失文件应报告错误：%v", err)
	}
}
