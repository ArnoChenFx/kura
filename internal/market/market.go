// Package market 读取 skills.sh 的技能榜单（只读，不需要密钥）。
//
// 接口形状：GET {base}/api/skills/{view}/{page}，page 从 0 开始，返回
// {"skills":[{"source","skillId","name","installs"}],"total","hasMore","page"}。
// view 是 all-time（总榜）、hot（热度）或 trending（趋势）。它不是 skills.sh
// 官方文档化的契约，来源与尚未核对的部分见 docs/note/skills-market.md，
// 所以解析从宽：缺字段或不合法的条目跳过，整页结构不对才报错。
//
// 本包只负责读数据：不碰磁盘，不依赖 UI；安装走 store.AddSkillByName。
package market

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// DefaultBaseURL 是 skills.sh 的地址。
const DefaultBaseURL = "https://skills.sh"

// View 是榜单的排序方式。
type View string

const (
	ViewAllTime  View = "all-time" // 总榜：按累计安装量
	ViewHot      View = "hot"      // 热度
	ViewTrending View = "trending" // 趋势
)

// maxPageBytes 限制一页响应的大小：一页只有几十到一百来条记录，远小于它。
// 超过就报错，不静默截断。
const maxPageBytes = 4 << 20

// requestTimeout 是单次请求的上限（调用方传入的 ctx 可以更早结束）。
const requestTimeout = 20 * time.Second

// 错误：身份由 key 决定（errors.Is 跨语言成立），文字随当前语言现取。
var (
	ErrUnknownView = i18n.NewError("marketErrUnknownView", func() string { return locales.MarketErrUnknownView() })
	ErrBadPage     = i18n.NewError("marketErrBadPage", func() string { return locales.MarketErrBadPage() })
	ErrDecode      = i18n.NewError("marketErrDecode", func() string { return locales.MarketErrDecode() })
	ErrTooLarge    = i18n.NewError("marketErrTooLarge", func() string { return locales.MarketErrTooLarge() })
)

// Skill 是榜单里的一个技能。
type Skill struct {
	Source   string // 来源仓库，owner/repo
	SkillID  string // skills.sh 上的技能标识（展示与排查用）
	Name     string // 技能名；安装时用它在仓库里找技能
	Installs int64  // 安装次数（接口报告的值，仅供显示与排序参考）
}

// Page 是榜单的一页。
type Page struct {
	Skills  []Skill
	Total   int  // 榜单总数（接口报告的值，仅供显示）
	HasMore bool // 后面还有页
}

// Client 访问 skills.sh 的榜单接口。用 New 创建。
type Client struct {
	base string
	http *http.Client
}

// New 创建访问 base 的客户端；base 为空时用 DefaultBaseURL。测试里传
// httptest 服务器的地址。
func New(base string) *Client {
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: requestTimeout},
	}
}

// List 读取榜单 view 的第 page 页（从 0 开始）。ctx 取消时立即返回。
func (c *Client) List(ctx context.Context, view View, page int) (Page, error) {
	if !knownView(view) {
		return Page{}, ErrUnknownView
	}
	if page < 0 {
		return Page{}, ErrBadPage
	}
	body, err := c.get(ctx, c.base+"/api/skills/"+string(view)+"/"+strconv.Itoa(page), maxPageBytes)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

// get 请求 endpoint 并返回响应体。非 2xx 报 marketErrStatus（带状态码）；超过 limit
// 字节报 ErrTooLarge，不静默截断；网络与读取错误报 marketErrRequest。
func (c *Client) get(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, i18n.Wrapf(err, "marketErrRequest", func() string { return locales.MarketErrRequest() })
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kura")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, i18n.Wrapf(err, "marketErrRequest", func() string { return locales.MarketErrRequest() })
	}
	defer resp.Body.Close()
	if code := resp.StatusCode; code < 200 || code > 299 {
		return nil, i18n.NewError("marketErrStatus", func() string { return locales.MarketErrStatus(code) })
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, i18n.Wrapf(err, "marketErrRequest", func() string { return locales.MarketErrRequest() })
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	return body, nil
}

// knownView 报告 view 是不是 skills.sh 的三个榜单之一。
func knownView(view View) bool {
	switch view {
	case ViewAllTime, ViewHot, ViewTrending:
		return true
	}
	return false
}

// wirePage 与 wireSkill 是接口的原始形状。
type wirePage struct {
	Skills  []wireSkill `json:"skills"`
	Total   int         `json:"total"`
	HasMore bool        `json:"hasMore"`
}

type wireSkill struct {
	Source   string `json:"source"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
}

// decodePage 解析一页响应。顶层结构不对（不是对象、skills 缺失或不是数组）
// 报 ErrDecode；单条记录不合法只跳过它。
func decodePage(body []byte) (Page, error) {
	var w wirePage
	if err := json.Unmarshal(body, &w); err != nil {
		return Page{}, i18n.Wrapf(err, "marketErrDecode", func() string { return locales.MarketErrDecode() })
	}
	if w.Skills == nil {
		return Page{}, ErrDecode
	}
	p := Page{Total: w.Total, HasMore: w.HasMore}
	for _, ws := range w.Skills {
		if sk, ok := toSkill(ws); ok {
			p.Skills = append(p.Skills, sk)
		}
	}
	return p, nil
}

// toSkill 把一条原始记录变成 Skill。来源不是 owner/repo 或没有名字的记录
// 装不了，跳过（false）。
func toSkill(w wireSkill) (Skill, bool) {
	owner, repo, found := strings.Cut(w.Source, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") ||
		strings.ContainsAny(w.Source, " \t\\") {
		return Skill{}, false
	}
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return Skill{}, false
	}
	id := strings.TrimSpace(w.SkillID)
	if id == "" {
		id = name
	}
	installs := w.Installs
	if installs < 0 {
		installs = 0
	}
	return Skill{Source: w.Source, SkillID: id, Name: name, Installs: installs}, true
}
