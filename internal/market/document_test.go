package market

// 技能详情（下载接口）的测试：httptest 本地服务器，不联网。

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// TestToSkillSlugMatchesCLI 验证 slug 规则与 vercel-labs/skills 的 toSkillSlug 一致。
func TestToSkillSlugMatchesCLI(t *testing.T) {
	cases := map[string]string{
		"vercel-react-best-practices": "vercel-react-best-practices",
		"Vercel React Best Practices": "vercel-react-best-practices",
		"react_native  tips":          "react-native-tips",
		"C++ & Go!":                   "c-go",
		"--Edge--":                    "edge",
		"a - b":                       "a-b",
		"中文":                          "",
		"":                            "",
	}
	for in, want := range cases {
		if got := toSkillSlug(in); got != want {
			t.Errorf("toSkillSlug(%q) = %q，想要 %q", in, got, want)
		}
	}
}

// TestDocumentRequestsDownloadRoute 验证请求的路径：owner 与 repo 小写，slug 由技能名算出，
// 不带任何认证头。
func TestDocumentRequestsDownloadRoute(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"files":[{"path":"SKILL.md","contents":"# hi"}],"hash":"h"}`)
	if _, err := New(f.srv.URL).Document(context.Background(), "Vercel-Labs/Agent-Skills", "My Skill"); err != nil {
		t.Fatalf("Document: %v", err)
	}
	if got := f.requests(); len(got) != 1 || got[0] != "/api/download/vercel-labs/agent-skills/my-skill" {
		t.Errorf("请求路径 = %v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.heads[0].Get("Authorization") != "" {
		t.Errorf("不应带认证头")
	}
}

// TestDocumentParsesSkillMDAndFiles 验证解析：SKILL.md 原文、描述、排序后的文件列表与哈希。
// 子目录里的 SKILL.md 不算技能的入口。
func TestDocumentParsesSkillMDAndFiles(t *testing.T) {
	body := `{"files":[
		{"path":"references/guide.md","contents":"guide"},
		{"path":"SKILL.md","contents":"---\nname: tdd\ndescription: \"Write tests first\"\n---\n\n# TDD\n"},
		{"path":"examples/SKILL.md","contents":"nested"}
	],"hash":"abc123"}`
	f := newFakeSkills(t, http.StatusOK, body)
	d, err := New(f.srv.URL).Document(context.Background(), "owner/repo", "tdd")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if !strings.HasPrefix(d.SkillMD, "---\nname: tdd") || !strings.Contains(d.SkillMD, "# TDD") {
		t.Errorf("SKILL.md 原文不对：%q", d.SkillMD)
	}
	if d.Description != "Write tests first" {
		t.Errorf("描述 = %q", d.Description)
	}
	want := []string{"examples/SKILL.md", "references/guide.md", "SKILL.md"}
	sort.Strings(want)
	if strings.Join(d.Files, " ") != strings.Join(want, " ") {
		t.Errorf("文件列表 = %v", d.Files)
	}
	if d.Hash != "abc123" {
		t.Errorf("哈希 = %q", d.Hash)
	}
}

// TestDocumentWithoutSkillMD 验证没有 SKILL.md 不是错误：正文为空，其余信息照常返回。
func TestDocumentWithoutSkillMD(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"files":[{"path":"README.md","contents":"hi"}],"hash":"h"}`)
	d, err := New(f.srv.URL).Document(context.Background(), "owner/repo", "plain")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if d.SkillMD != "" || d.Description != "" || len(d.Files) != 1 {
		t.Errorf("没有 SKILL.md 时的结果不对：%+v", d)
	}
}

// TestDocumentRejectsBadInputWithoutRequest 验证非法的来源与换不出 slug 的名字在发请求前拒绝。
func TestDocumentRejectsBadInputWithoutRequest(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"files":[]}`)
	c := New(f.srv.URL)
	if _, err := c.Document(context.Background(), "noslash", "tdd"); !errors.Is(err, ErrBadSource) {
		t.Errorf("来源不对应为 ErrBadSource，得到 %v", err)
	}
	if _, err := c.Document(context.Background(), "owner/repo", "中文"); !errors.Is(err, ErrNoSlug) {
		t.Errorf("换不出 slug 应为 ErrNoSlug，得到 %v", err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("非法输入不应发请求，实际 %d 次", n)
	}
}

// TestDocumentReportsStatusAndStructure 验证 404 报告状态码；缺少 files 数组是结构错误。
func TestDocumentReportsStatusAndStructure(t *testing.T) {
	notFound := newFakeSkills(t, http.StatusNotFound, "not found")
	if _, err := New(notFound.srv.URL).Document(context.Background(), "owner/repo", "gone"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 应报告状态码，得到 %v", err)
	}
	bad := newFakeSkills(t, http.StatusOK, `{"hash":"h"}`)
	if _, err := New(bad.srv.URL).Document(context.Background(), "owner/repo", "tdd"); !errors.Is(err, ErrDecode) {
		t.Errorf("缺少 files 应为 ErrDecode，得到 %v", err)
	}
}

// TestDocumentRejectsOversizedBody 验证超过快照大小上限直接报错。
func TestDocumentRejectsOversizedBody(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, strings.Repeat("x", maxDocBytes+1))
	if _, err := New(f.srv.URL).Document(context.Background(), "owner/repo", "tdd"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("应为 ErrTooLarge，得到 %v", err)
	}
}

// TestFrontmatterDescription 覆盖 description 的三种写法与缺失的情况。
func TestFrontmatterDescription(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{"plain", "---\nname: x\ndescription: Plain text\n---\n# x", "Plain text"},
		{"quoted", "---\ndescription: 'Single quoted'\n---", "Single quoted"},
		{"folded", "---\nname: x\ndescription: >\n  first part\n  second part\nlicense: MIT\n---\n# x", "first part second part"},
		{"no frontmatter", "# x\ndescription: not frontmatter", ""},
		{"no description", "---\nname: x\n---\n", ""},
		{"crlf", "---\r\ndescription: CRLF\r\n---\r\n", "CRLF"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		if got := frontmatterDescription(tc.md); got != tc.want {
			t.Errorf("%s: frontmatterDescription = %q，想要 %q", tc.name, got, tc.want)
		}
	}
}
