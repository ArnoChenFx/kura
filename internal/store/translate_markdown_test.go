package store

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 替换正文词，同时断言请求里没有代码、链接与元数据的真实内容。
func TestGoogleMarkdownProtectsExecutableAndFormatting(t *testing.T) {
	src := "---\r\nname: sample-secret\r\n---\r\n" +
		"# Guide\r\n\r\nRead **instructions** and `run-secret --flag`.\r\n" +
		"Use [documentation](https://example.invalid/private \"Private title\").\r\n" +
		"| Name | Description |\r\n| :--- | ---: |\r\n| alpha | Helpful |\r\n\r\n" +
		"```sh\r\nprintf 'secret-command'\r\n\r\n```\r\n" +
		"    indented-secret\r\n" +
		"Visit https://example.invalid/private and read [ref].\r\n" +
		"[ref]: https://example.invalid/hidden\r\n"
	replacer := strings.NewReplacer(
		"Guide", "指南", "Read", "阅读", "instructions", "说明", "and", "与",
		"Use", "使用", "documentation", "文档", "Name", "名称",
		"Description", "描述", "Helpful", "有用", "Visit", "访问", "read", "阅读",
	)
	calls := 0
	got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, body string) (string, error) {
		calls++
		for _, secret := range []string{"sample-secret", "run-secret", "secret-command", "indented-secret", "example.invalid", "Private title", "[ref]"} {
			if strings.Contains(body, secret) {
				t.Errorf("protected content was sent: %q", secret)
			}
		}
		return replacer.Replace(body), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "---\r\nname: sample-secret\r\n---\r\n" +
		"# 指南\r\n\r\n阅读 **说明** 与 `run-secret --flag`.\r\n" +
		"使用 [文档](https://example.invalid/private \"Private title\").\r\n" +
		"| 名称 | 描述 |\r\n| :--- | ---: |\r\n| alpha | 有用 |\r\n\r\n" +
		"```sh\r\nprintf 'secret-command'\r\n\r\n```\r\n" +
		"    indented-secret\r\n" +
		"访问 https://example.invalid/private 与 阅读 [ref].\r\n" +
		"[ref]: https://example.invalid/hidden\r\n"
	if got != want {
		t.Fatalf("format or protected content changed:\n got %q\nwant %q", got, want)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want one batched request", calls)
	}
}

func TestGoogleMarkdownCodeOnlyDoesNotRequest(t *testing.T) {
	for _, src := range []string{
		"```sh\nrun-secret\n```\n",
		"> ```sh\n> run-secret\n> ```\n",
		">     indented-secret\n>     more-secret\n",
		"- ```sh\n  run-secret\n  ```\n",
		"`run-secret`\n\n    more-secret\n",
		"---\nname: hidden\n---\n",
		"---\nname: hidden\n",
		"https://example.invalid/path",
		"<!--\nsecret-comment\n-->\n",
		"<pre class=\"code\">\nrun-secret\n</pre>\n",
		" \r\n\r\n",
	} {
		got, err := translateGoogleMarkdown(context.Background(), src, func(context.Context, string) (string, error) {
			t.Fatal("code-only text must not make a request")
			return "", nil
		})
		if err != nil || got != src {
			t.Fatalf("src=%q, got=%q, err=%v", src, got, err)
		}
	}
}

func TestGoogleMarkdownProtectsMultilineAndNestedSyntax(t *testing.T) {
	src := "Read ``run `literal`\nsecret`` and [documentation](https://example.invalid/a(b) \"title\").\n" +
		"Read [documentation][private-ref] and [^footnote].\n" +
		"Read \\*literal\\* and <https://example.invalid/private>.\n" +
		"Read ![documentation](image.png).\n"
	got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, body string) (string, error) {
		for _, secret := range []string{"secret", "private-ref", "footnote", "image.png", "example.invalid"} {
			if strings.Contains(body, secret) {
				t.Fatalf("protected text sent: %s", secret)
			}
		}
		return strings.NewReplacer("Read", "阅读", "documentation", "文档").Replace(body), nil
	})
	want := strings.NewReplacer("Read", "阅读", "documentation", "文档").Replace(src)
	if err != nil || got != want {
		t.Fatalf("got %q err %v; want %q", got, err, want)
	}
}

func TestGoogleMarkdownRejectsDamagedMarkers(t *testing.T) {
	markers := regexp.MustCompile(`ZXQKURA[0-9]+QXZ`)
	for name, damage := range map[string]func(string) string{
		"missing":   func(s string) string { return strings.Replace(s, markers.FindString(s), "", 1) },
		"duplicate": func(s string) string { return s + markers.FindString(s) },
		"renamed":   func(s string) string { return strings.ReplaceAll(s, "ZXQKURA", "BROKEN") },
		"reordered": func(s string) string {
			all := markers.FindAllString(s, -1)
			return strings.NewReplacer(all[0], all[1], all[1], all[0]).Replace(s)
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := translateGoogleMarkdown(context.Background(), "# Read `command` now.", func(_ context.Context, body string) (string, error) {
				return damage(body), nil
			})
			if err == nil || out != "" {
				t.Fatalf("damaged markers accepted: out=%q err=%v", out, err)
			}
			if strings.Contains(err.Error(), "command") {
				t.Fatal("error leaked source text")
			}
		})
	}
}

func TestGoogleMarkdownMarkerCollisionAndServiceWhitespace(t *testing.T) {
	src := "# Read ZXQKURA0QXZ with `secret` now.\n"
	got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, body string) (string, error) {
		if !strings.Contains(body, "ZXQKURAX") {
			t.Fatal("source token collision was not avoided")
		}
		body = strings.ReplaceAll(body, " ", " \n ")
		return strings.ReplaceAll(body, "Read", "阅读"), nil
	})
	// 服务可以改变正文空白，但 Markdown 标记邻接空白与原样代码必须保留。
	if err != nil || !strings.HasPrefix(got, "# 阅读") || !strings.Contains(got, " `secret` ") ||
		!strings.Contains(got, "ZXQKURA0QXZ") || !strings.HasSuffix(got, ".\n") {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestGoogleMarkdownCancellationAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := translateGoogleMarkdown(ctx, "Read this.", func(context.Context, string) (string, error) {
		t.Fatal("cancelled task made a request")
		return "", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	failure := errors.New("service unavailable")
	out, err := translateGoogleMarkdown(context.Background(), "# Read this.", func(context.Context, string) (string, error) {
		return "", failure
	})
	if !errors.Is(err, failure) || out != "" {
		t.Fatalf("got %q, err %v", out, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	out, err = translateGoogleMarkdown(ctx, "# Read this.", func(_ context.Context, body string) (string, error) {
		cancel()
		return body, nil
	})
	if !errors.Is(err, context.Canceled) || out != "" {
		t.Fatalf("cancel during request: got %q, err %v", out, err)
	}
}

func TestGoogleMarkdownLongParagraphBatchesWithoutChangingSource(t *testing.T) {
	src := "# Guide\n\n" + strings.Repeat("Read the documentation with `secret-command`. ", 1000)
	calls := 0
	got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, body string) (string, error) {
		calls++
		if strings.Contains(body, "secret-command") {
			t.Fatal("command leaked to translator")
		}
		return body, nil
	})
	if err != nil || got != src || calls != 1 {
		t.Fatalf("long paragraph: calls=%d unchanged=%t err=%v", calls, got == src, err)
	}
}

func TestFreeTranslationChunksKeepMarkdownMarkersWhole(t *testing.T) {
	// 特意让边界落在长正文后的标记附近，同时覆盖中文多字节与来源标记碰撞。
	src := "# Guide ZXQKURA" + strings.Repeat("X", 1600) + "\n\n" +
		strings.Repeat("Read the documentation 文档 ", 200) + "`secret-command`\n" +
		strings.Repeat("a", 2200) + " [documentation](https://example.invalid/secret)\n" +
		strings.Repeat("Read **instructions** with `secret-command`.\n", 100)
	markers := regexp.MustCompile(`ZXQKURA(?:X[0-9]+P)?[0-9]+QXZ`)
	for _, limit := range []int{googleMaxTextBytes, microsoftMaxTextBytes} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			got, err := translateGoogleMarkdown(context.Background(), src, func(_ context.Context, body string) (string, error) {
				whole := markers.FindAllString(body, -1)
				var chunkMarkers []string
				var joined strings.Builder
				for _, part := range freeTranslationChunks(body, limit) {
					if len(part) > limit {
						t.Fatalf("chunk has %d bytes, limit %d", len(part), limit)
					}
					chunkMarkers = append(chunkMarkers, markers.FindAllString(part, -1)...)
					joined.WriteString(part)
				}
				if strings.Join(whole, "|") != strings.Join(chunkMarkers, "|") {
					t.Fatal("a protected marker was split across requests")
				}
				return joined.String(), nil
			})
			if err != nil || got != src {
				t.Fatalf("chunks changed Markdown: unchanged=%t err=%v", got == src, err)
			}
		})
	}
}
