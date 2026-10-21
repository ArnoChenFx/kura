package ui

// 本地全文与市场全文共享进度、停止和续译控件，状态仅属于当前显示的文档。

import (
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

func (a *App) viewDocTranslation(c *myui.Context, key, src string) {
	if !a.trans.on || !a.settings.Ready() || src == "" {
		a.viewMarkdown(c, src)
		return
	}
	a.TranslateDocAsync(key, src)
	e, ok := a.trans.get(key, src)
	t := c.Theme()
	switch {
	case !ok || e.pending:
		myui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(myui.Center).Wrap().Children(func() {
			loadingIndicator(c, locales.UiTranslateFullInProgress())
			label := locales.UiTranslateFullInProgress()
			if len(e.chunks) > 0 {
				label = locales.UiTranslateDocProgress(len(e.parts), len(e.chunks))
			}
			myui.Text(c, label).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			if myui.Button(c, locales.UiTranslateStop()).Clicked() {
				a.StopDocTranslation(key, src)
			}
		})
	case e.err != "" || e.paused:
		if e.err != "" {
			myui.Text(c, locales.UiTranslateFullFailed(e.err)).MaxLines(4).
				FontSize(t.FontSize - 1).TextColor(t.Danger)
		} else {
			myui.Text(c, locales.UiTranslateStopped()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		}
		myui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(myui.Center).Wrap().Children(func() {
			if len(e.chunks) > 0 {
				myui.Text(c, locales.UiTranslateDocProgress(len(e.parts), len(e.chunks))).
					FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			}
			if myui.Button(c, locales.UiTranslateResume()).Clicked() {
				a.ResumeDocTranslation(key, src)
			}
		})
	default:
		myui.Text(c, locales.UiTranslateNote(a.settings.LangLabel())).
			FontSize(t.FontSize - 2).TextColor(t.TextMuted)
	}
	if e.dst != "" {
		a.viewMarkdown(c, e.dst)
	} else {
		a.viewMarkdown(c, src)
	}
}
