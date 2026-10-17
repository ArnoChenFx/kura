package market

// 技能详情：skills.sh 的下载接口返回一个技能快照的全部文件，这里只取两样给
// 详情页用：SKILL.md 的原文（渲染成正文）与文件路径列表（只显示，不读内容）。
//
// 接口形状来自 vercel-labs/skills 的 src/blob.ts（fetchSkillDownload）：
//
//	GET {base}/api/download/{owner}/{repo}/{slug}
//	→ {"files":[{"path":"SKILL.md","contents":"..."}],"hash":"..."}
//
// path 相对技能目录；slug 由技能名按 toSkillSlug 算出（与 CLI 完全一致，
// 服务端按它定位快照）。同样未经实际请求核对，见 docs/note/skills-market.md。

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// maxDocBytes 限制下载接口一次响应的大小。快照包含技能的全部文件，比榜单页大得多，
// 但仍设上限：超过就报错，不把病态的大响应读进内存。
const maxDocBytes = 8 << 20

// 错误（身份由 key 决定，文字随当前语言现取）。
var (
	ErrBadSource = i18n.NewError("marketErrBadSource", func() string { return locales.MarketErrBadSource() })
	ErrNoSlug    = i18n.NewError("marketErrNoSlug", func() string { return locales.MarketErrNoSlug() })
)

// Document 是技能在 skills.sh 上的快照里详情页用到的部分。
type Document struct {
	Description string   // SKILL.md frontmatter 的 description（没有时为空串）
	SkillMD     string   // SKILL.md 的原文（没有时为空串）
	Files       []string // 技能目录里的文件路径（相对技能目录，已排序）
	Hash        string   // 快照哈希（接口报告的值，仅供显示）
}

// wireDoc 与 wireFile 是下载接口的原始形状。
type wireDoc struct {
	Files []wireFile `json:"files"`
	Hash  string     `json:"hash"`
}

type wireFile struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

// Document 读取技能 name（来源 source，即 owner/repo）的快照。owner 与 repo 按 CLI
// 的做法小写后请求。名字换不出链接用的 slug（比如全是中文）时报 ErrNoSlug，不发请求。
func (c *Client) Document(ctx context.Context, source, name string) (Document, error) {
	owner, repo, ok := splitSource(source)
	if !ok {
		return Document{}, ErrBadSource
	}
	slug := toSkillSlug(name)
	if slug == "" {
		return Document{}, ErrNoSlug
	}
	endpoint := c.base + "/api/download/" + url.PathEscape(strings.ToLower(owner)) + "/" +
		url.PathEscape(strings.ToLower(repo)) + "/" + slug
	body, err := c.get(ctx, endpoint, maxDocBytes)
	if err != nil {
		return Document{}, err
	}
	return parseDocument(body)
}

// splitSource 把 owner/repo 拆开；格式不对（没有斜杠、多了斜杠、含空白）报告 false。
func splitSource(source string) (owner, repo string, ok bool) {
	owner, repo, found := strings.Cut(source, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") ||
		strings.ContainsAny(source, " \t\\") {
		return "", "", false
	}
	return owner, repo, true
}

// parseDocument 解析下载接口的响应。files 缺失或不是数组是结构错误；没有 SKILL.md
// 不是错误（Document 里 SkillMD 为空，详情页照常显示其余信息）。
func parseDocument(body []byte) (Document, error) {
	var w wireDoc
	if err := json.Unmarshal(body, &w); err != nil {
		return Document{}, i18n.Wrapf(err, "marketErrDecode", func() string { return locales.MarketErrDecode() })
	}
	if w.Files == nil {
		return Document{}, ErrDecode
	}
	d := Document{Hash: w.Hash}
	for _, f := range w.Files {
		d.Files = append(d.Files, f.Path)
		if d.SkillMD == "" && !strings.Contains(f.Path, "/") && strings.EqualFold(f.Path, "SKILL.md") {
			d.SkillMD = f.Contents
		}
	}
	sort.Strings(d.Files)
	d.Description = frontmatterDescription(d.SkillMD)
	return d, nil
}

// toSkillSlug 与 vercel-labs/skills 的 toSkillSlug 一致：小写；空白（含 U+FEFF）与
// 下划线变成连字符；只留 a-z、0-9 与连字符；连续的连字符合并，首尾去掉。
func toSkillSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsSpace(r) || r == '_' || r == '\ufeff' {
			r = '-'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' }), "-")
}

// frontmatterDescription 取 SKILL.md 开头 frontmatter 里顶层的 description。支持单行
// （可带引号）与 > / | 块（续行合并成一行，供详情页概括用）；其余写法返回空串。
func frontmatterDescription(md string) string {
	md = strings.TrimPrefix(md, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return ""
		}
		if !strings.HasPrefix(line, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		if value == "" || value == ">" || value == "|" {
			var parts []string
			for _, next := range lines[i+1:] {
				if strings.TrimSpace(next) == "---" || (next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t")) {
					break
				}
				if s := strings.TrimSpace(next); s != "" {
					parts = append(parts, s)
				}
			}
			value = strings.Join(parts, " ")
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}
