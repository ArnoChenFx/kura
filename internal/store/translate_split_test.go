package store

// 全文翻译的分段：只在空行切、不切开围栏代码块、段内结构原样保留。

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitForTranslationEmpty(t *testing.T) {
	if got := SplitForTranslation("", 100); got != nil {
		t.Errorf("空文本应返回 nil，得到 %q", got)
	}
	if got := SplitForTranslation(" \n\n \t\n", 100); got != nil {
		t.Errorf("全空白应返回 nil，得到 %q", got)
	}
}

func TestSplitForTranslationKeepsFencedCodeWhole(t *testing.T) {
	doc := strings.Join([]string{
		"# 标题",
		"",
		"前言段落。",
		"",
		"```bash",
		"echo one",
		"",
		"echo two",
		"```",
		"",
		"结尾段落。",
	}, "\n")
	// 上限很小，逼着切分；代码块里的空行不能成为分界。
	parts := SplitForTranslation(doc, 10)
	var code string
	for _, p := range parts {
		if strings.Contains(p, "echo one") {
			code = p
		}
	}
	if !strings.Contains(code, "echo two") || !strings.Contains(code, "```bash") || !strings.HasSuffix(code, "```") {
		t.Errorf("代码块被切开了：%q", parts)
	}
	if got := strings.Join(parts, "\n\n"); !strings.Contains(got, "echo one\n\necho two") {
		t.Errorf("拼回去后代码块内的空行应保留，得到 %q", got)
	}
}

func TestSplitForTranslationRespectsLimitAndRejoins(t *testing.T) {
	var paras []string
	for i := 0; i < 12; i++ {
		paras = append(paras, strings.Repeat("字", 40)+" 段"+string(rune('A'+i)))
	}
	doc := strings.Join(paras, "\n\n")
	const limit = 150
	parts := SplitForTranslation(doc, limit)
	if len(parts) < 3 {
		t.Fatalf("应切成多段，得到 %d 段", len(parts))
	}
	for i, p := range parts {
		// 单段只要没超过上限（或只剩一个块）就合格。
		if n := utf8.RuneCountInString(p); n > limit && strings.Count(p, "\n\n") > 0 {
			t.Errorf("第 %d 段 %d 个字符，超过上限 %d 且含多个块", i, n, limit)
		}
	}
	if got := strings.Join(parts, "\n\n"); got != doc {
		t.Errorf("拼回去应与原文一致\n got: %q\nwant: %q", got, doc)
	}
}

func TestSplitForTranslationOversizedBlockStaysWhole(t *testing.T) {
	big := strings.Repeat("长", 500)
	parts := SplitForTranslation("短\n\n"+big+"\n\n尾", 50)
	found := false
	for _, p := range parts {
		if p == big {
			found = true
		}
	}
	if !found {
		t.Errorf("超长的单块应整块保留，不硬切：%d 段", len(parts))
	}
}

func TestSplitForTranslationTildeFence(t *testing.T) {
	doc := "~~~\nkeep\n\nthis too\n~~~\n\nafter"
	parts := SplitForTranslation(doc, 5)
	if len(parts) != 2 || !strings.Contains(parts[0], "this too") {
		t.Errorf("~~~ 围栏应同样保护代码块，得到 %q", parts)
	}
}
