package ui

// 市场快照的显示与翻译使用同一份受限源码，不能把显示范围之外的正文发给服务。

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

func TestMarketDocumentLimitsCacheAndTranslation(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings, a.trans.on = trReadySettings(), true
	sk := market.Skill{Source: "owner/repo", Name: "large"}
	desc := "Description parsed before truncation."
	src := strings.Repeat("a", fullTextMaxSource-1) + "文" + "beyond-display-limit"
	a.marketDocFetch = func(context.Context, string, string) (market.Document, error) {
		return market.Document{Description: desc, SkillMD: src}, nil
	}
	a.loadMarketDocAsync(sk)
	a.flush()
	d := a.market.docOr(marketKey(sk))
	if !d.loaded || !d.truncated || len(d.doc.SkillMD) > fullTextMaxSource || !utf8.ValidString(d.doc.SkillMD) {
		t.Fatalf("市场缓存未限制在UTF-8边界：loaded=%v cut=%v bytes=%d", d.loaded, d.truncated, len(d.doc.SkillMD))
	}
	if d.doc.Description != desc {
		t.Fatal("截断正文不能覆盖已经解析出的描述")
	}
	a.market.screen, a.market.detail = marketDetail, sk
	var sent []string
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		sent = append(sent, text)
		if len(text) > fullTextMaxSource || strings.Contains(text, "beyond-display-limit") {
			t.Errorf("翻译请求包含显示范围之外的正文：bytes=%d", len(text))
		}
		return text, nil
	}
	a.ensureMarketTranslations()
	a.flush()
	if len(sent) != 2 || sent[0] != desc || sent[1] != d.doc.SkillMD {
		t.Fatalf("翻译应只接收描述和受限正文，requests=%d", len(sent))
	}
}

func TestMarketDocumentShowsReadTruncationNotice(t *testing.T) {
	a, _ := newTestApp(t)
	sk := market.Skill{Source: "owner/repo", Name: "large"}
	d := &marketDoc{loaded: true, truncated: true, doc: market.Document{SkillMD: "# Short display fixture"}}
	tt := myui.NewTester(func(c *myui.Context) {
		myui.Column(c).Fill().Children(func() { a.viewMarketDoc(c, sk, d) })
	}, 720, 480)
	tt.Frame()
	if !tt.HasText(locales.UiMarkdownTruncated()) {
		t.Fatalf("读取阶段已经截断，显示端仍应提示：%v", tt.Texts())
	}
}
