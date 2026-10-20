package store

// 全文翻译的分段：只在空行切、不切开围栏代码块、段内结构原样保留。

import (
	"context"
	"encoding/json"
	"net/http"
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

// TestSplitIndentedFenceIsNotAFence 验证 CommonMark 规则：4 个及以上空格缩进的
// ``` 是缩进代码块，不是围栏。把它当围栏会让后面的正文与代码全并进一块，
// 以一次超限请求发出（PR #4 评审缺陷 4）。
func TestSplitIndentedFenceIsNotAFence(t *testing.T) {
	doc := strings.Join([]string{
		"    ```",
		"",
		"第一段正文，不在代码块里。",
		"",
		"第二段正文，同样不在代码块里。",
		"",
		"```bash",
		"echo x",
		"```",
	}, "\n")
	blocks := markdownBlocks(doc)
	if len(blocks) < 4 {
		t.Fatalf("缩进的围栏不应吞掉后面的空行分界，想要至少 4 块，得到 %d：%q", len(blocks), blocks)
	}
	// 按小上限切分时，没有一块被并成超限的大块。
	for _, p := range SplitForTranslation(doc, 30) {
		if utf8.RuneCountInString(p) > 30 && !strings.Contains(p, "```bash") {
			t.Errorf("出现超限块 %q", p)
		}
	}
}

// TestFenceMarkerIndentRules 验证围栏标记的缩进与 info 串规则。
func TestFenceMarkerIndentRules(t *testing.T) {
	cases := []struct {
		line, want string
	}{
		{"```", "```"},
		{"   ```go", "```"}, // 3 个空格仍是围栏
		{"    ```", ""},     // 4 个空格是缩进代码块
		{"\t```", ""},       // 制表符缩进同样
		{"~~~~", "~~~~"},
		{"``", ""},
		{"``` a`b", ""}, // 反引号围栏的 info 串里不能有反引号
		{"~~~ a`b", "~~~"},
		{"text ```", ""},
	}
	for _, c := range cases {
		if got := fenceMarker(c.line); got != c.want {
			t.Errorf("fenceMarker(%q) = %q，想要 %q", c.line, got, c.want)
		}
	}
	closing := []struct {
		line, fence string
		want        bool
	}{
		{"```", "```", true},
		{"````", "```", true},
		{"``", "```", false},
		{"   ```  ", "```", true},
		{"    ```", "```", false}, // 缩进的闭合行不算
		{"``` x", "```", false},
		{"~~~", "```", false},
	}
	for _, c := range closing {
		if got := isClosingFence(c.line, c.fence); got != c.want {
			t.Errorf("isClosingFence(%q, %q) = %v，想要 %v", c.line, c.fence, got, c.want)
		}
	}
}

// TestSplitUnclosedFenceKeepsFollowingText 验证未闭合围栏时后面的内容不被丢弃。
func TestSplitUnclosedFenceKeepsFollowingText(t *testing.T) {
	doc := "前言\n\n```\ncode\n\n后续正文不能丢"
	var all string
	for _, b := range markdownBlocks(doc) {
		all += b + "\n"
	}
	for _, want := range []string{"前言", "code", "后续正文不能丢"} {
		if !strings.Contains(all, want) {
			t.Errorf("未闭合围栏后丢了 %q：%q", want, all)
		}
	}
}

// TestSplitIndentedClosingFenceDoesNotClose 验证：围栏内缩进 4 格的 ``` 不是
// 闭合围栏，围栏继续到真正的闭合行（其间的空行不分界）。
func TestSplitIndentedClosingFenceDoesNotClose(t *testing.T) {
	doc := "```\na\n    ```\n\nb\n```\n\n后文"
	blocks := markdownBlocks(doc)
	if len(blocks) != 2 || !strings.Contains(blocks[0], "\n\nb\n") {
		t.Errorf("缩进的 ``` 不应闭合围栏，得到 %q", blocks)
	}
}

// 走真实分段和免费服务派发，避免单独测试保护器时漏掉分段造成的上下文丢失。
func TestSplitFreeTranslationPreservesProtectedBlocks(t *testing.T) {
	longCode := strings.Repeat("PROTECTEDCONTENT\n", 130)
	cases := map[string]string{
		"list-fence": "- ```python\n" + strings.Repeat("  PROTECTEDCONTENT\n", 130) +
			"\n  CANARYCODE\n  ```",
		"list-fence-four-spaces": "-   ```python\n" + strings.Repeat("    PROTECTEDCONTENT\n", 130) +
			"\n    CANARYCODE\n    ```",
		"ordered-list-fence": "10. ```python\n" + strings.Repeat("    PROTECTEDCONTENT\n", 130) +
			"\n    CANARYCODE\n    ```",
		"indented-list-fence": "  - ```python\n" + strings.Repeat("    PROTECTEDCONTENT\n", 130) +
			"\n    CANARYCODE\n    ```",
		"root-fence-with-list-marker":  "```text\n- ```\n" + longCode + "\nCANARYCODE\n```",
		"root-fence-with-quote-marker": "```text\n> ```\n" + longCode + "\nCANARYCODE\n```",
		"quote-fence-with-extra-quote": "> ```text\n> > ```\n" + strings.Repeat("> PROTECTEDCONTENT\n", 130) +
			">\n> CANARYCODE\n> ```",
		"quoted-list-fence": "> -   ```python\n" + strings.Repeat(">     PROTECTEDCONTENT\n", 130) +
			">\n>     CANARYCODE\n>     ```",
		"raw-html":    "<pre>\n" + longCode + "\nCANARYCODE\n</pre>",
		"comment":     "<!--\n" + longCode + "\nCANARYCODE\n-->",
		"frontmatter": "---\nnotes: |\n" + strings.Repeat("  PROTECTEDCONTENT\n", 130) + "\nCANARYCODE: metadata\n---",
		"code-span":   "Read instructions. ``" + longCode + "\nCANARYCODE``",
	}
	for name, protected := range cases {
		t.Run(name, func(t *testing.T) {
			src := protected + "\n\nRead instructions.\n\n" +
				strings.Repeat("Normal prose. ", 150) + "\n\nRead instructions."
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				plain := r.URL.Query().Get("q")
				if strings.Contains(plain, "CANARYCODE") || strings.Contains(plain, "PROTECTEDCONTENT") {
					t.Fatal("分段后的代码或元数据进入了翻译请求")
				}
				out := strings.ReplaceAll(plain, "Read instructions.", "Translated prose.")
				body, err := json.Marshal(map[string]any{
					"sentences": []map[string]string{{"trans": out}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return googleTestResponse(http.StatusOK, string(body)), nil
			})
			chunks := SplitForTranslation(src, 1800)
			if len(chunks) < 3 {
				t.Fatalf("应只保留保护区完整，不能把整篇长文合为一段：%d", len(chunks))
			}
			var translated []string
			for _, chunk := range chunks {
				out, err := (&Settings{Provider: TranslationGoogle}).Translate(context.Background(), chunk)
				if err != nil {
					t.Fatal(err)
				}
				translated = append(translated, out)
			}
			want := strings.ReplaceAll(src, "Read instructions.", "Translated prose.")
			if got := strings.Join(translated, "\n\n"); got != want {
				t.Fatal("翻译后应保留完整代码、元数据和 Markdown，只改变普通正文")
			}
			if calls == 0 {
				t.Fatal("普通正文仍须调用翻译服务")
			}
		})
	}
}

func TestIndentedCodeDoesNotHideFollowingProse(t *testing.T) {
	for _, line := range []string{"    ```", "    <pre>", "    <!--", "\t<pre>", ">     <!--"} {
		t.Run(line, func(t *testing.T) {
			src := line + "\n\nRead instructions.\n\n    CANARYCODE"
			calls := 0
			got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, plain string) (string, error) {
				calls++
				if strings.Contains(plain, "CANARYCODE") {
					t.Fatal("缩进代码不能进入翻译请求")
				}
				return strings.ReplaceAll(plain, "Read instructions.", "Translated prose."), nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("缩进代码后的普通正文应被翻译：calls=%d err=%v", calls, err)
			}
			if got != strings.ReplaceAll(src, "Read instructions.", "Translated prose.") {
				t.Fatal("缩进代码应原样保留，普通正文应翻译")
			}
		})
	}
}
