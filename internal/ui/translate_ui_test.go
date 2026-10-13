// 技能描述翻译的界面测试。翻译函数一律注入假实现（App.translate），
// 绝不发真实网络请求；全部数据造在 t.TempDir() 里；假密钥写 test-key-not-real。
//
// 翻译策略（被测对象）：只翻译「当前选中的技能」的描述，它在详情与它自己的
// 卡片里共用一次请求；其余卡片仍是原文。
package ui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// fakeTranslator 是注入的假翻译：记录调用，返回「[译]」+原文，或固定的错误。
// 测试里 spawn 是同步的，不需要锁。
type fakeTranslator struct {
	calls int
	texts []string
	err   error
}

func (f *fakeTranslator) translate(_ context.Context, text string) (string, error) {
	f.calls++
	f.texts = append(f.texts, text)
	if f.err != nil {
		return "", f.err
	}
	return "[译]" + text, nil
}

// trReadySettings 是「已配置好」的设置（假地址、假密钥）。
func trReadySettings() store.Settings {
	return store.Settings{BaseURL: "https://example.invalid/v1", APIKey: fakeKey, Model: "fake-model", TargetLang: "zh-CN"}
}

// trApp 造一个带两个有描述技能的应用，并立刻注入假翻译。ready 为真时
// 设置已填好；为假时保持未配置。返回假翻译与已启动的 Tester。
func trApp(t *testing.T, ready bool) (*App, *store.Config, *fakeTranslator, *myui.Tester) {
	t.Helper()
	a, cfg := newTestApp(t)
	f := &fakeTranslator{}
	a.translate = f.translate // 先于一切：绝不让真实实现被调用
	if ready {
		a.settings = trReadySettings()
	}
	seedDescribed(t, cfg, "alpha", "Reads PDF files.", nil)
	seedDescribed(t, cfg, "beta", "Writes release notes.", nil)
	tt := showApp(a, 1280, 800)
	return a, cfg, f, tt
}

// TestTranslateNotConfigured 验证：翻译 API 未配置时切到译文不发任何请求，
// 给出引导，「打开设置」能打开弹窗。
func TestTranslateNotConfigured(t *testing.T) {
	a, _, f, tt := trApp(t, false)
	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()

	if !a.trans.on {
		t.Fatal("点「译文」后应处于译文模式")
	}
	if f.calls != 0 {
		t.Errorf("未配置时不应调用翻译，调用了 %d 次", f.calls)
	}
	if !detHasText(tt, "先在设置里填翻译 API") {
		t.Errorf("未配置时应给出引导，文本 %v", tt.Texts())
	}
	// 描述仍是原文，不留白。
	if countText(tt, "Reads PDF files.") != 2 {
		t.Errorf("未配置时详情与卡片应仍显示原文，文本 %v", tt.Texts())
	}
	click(t, a, tt, "打开设置")
	if !a.settingsUI.open {
		t.Error("点「打开设置」应打开设置弹窗")
	}
	if f.calls != 0 {
		t.Errorf("打开设置后仍不应调用翻译，调用了 %d 次", f.calls)
	}
}

// TestTranslateShowsTranslation 验证：就绪后切到译文，详情描述与选中技能
// 的卡片描述都显示译文，其余卡片保持原文；切回「原文」都恢复。
func TestTranslateShowsTranslation(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	click(t, a, tt, "alpha")
	if f.calls != 0 {
		t.Fatalf("只是选中技能不应翻译，调用了 %d 次", f.calls)
	}

	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	if f.calls != 1 || f.texts[0] != "Reads PDF files." {
		t.Fatalf("应只为选中的技能翻译一次，calls=%d texts=%v", f.calls, f.texts)
	}
	// 详情 + 卡片各一处译文；alpha 的原文不再出现。
	if got := countText(tt, "[译]Reads PDF files."); got != 2 {
		t.Errorf("详情与卡片应各显示一处译文，得到 %d 处，文本 %v", got, tt.Texts())
	}
	if countText(tt, "Reads PDF files.") != 0 {
		t.Errorf("译文模式下选中技能不应再显示原文，文本 %v", tt.Texts())
	}
	// 策略：其余卡片不翻译，保持原文。
	if !tt.HasText("Writes release notes.") || tt.HasText("[译]Writes release notes.") {
		t.Errorf("未选中的卡片应保持原文，文本 %v", tt.Texts())
	}
	if !detHasText(tt, "译文 · 简体中文") {
		t.Errorf("详情应标明译文语言，文本 %v", tt.Texts())
	}

	// 切回原文：原文回来，译文消失，且不再请求。
	click(t, a, tt, "原文")
	if got := countText(tt, "Reads PDF files."); got != 2 {
		t.Errorf("切回原文后详情与卡片应各显示一处原文，得到 %d 处，文本 %v", got, tt.Texts())
	}
	if tt.HasText("[译]Reads PDF files.") {
		t.Errorf("切回原文后不应再显示译文，文本 %v", tt.Texts())
	}
	if f.calls != 1 {
		t.Errorf("切回原文不应再次翻译，calls=%d", f.calls)
	}
}

// TestTranslateCachesAcrossToggles 验证：同一技能来回切原文/译文，以及
// 换选再换回，已翻过的都命中缓存，调用次数不增长。
func TestTranslateCachesAcrossToggles(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	for i := 0; i < 3; i++ {
		click(t, a, tt, "原文")
		click(t, a, tt, "译文")
		a.settle()
		tt.Frame()
	}
	if f.calls != 1 {
		t.Fatalf("来回切换不应重复翻译，calls=%d", f.calls)
	}
	if countText(tt, "[译]Reads PDF files.") != 2 {
		t.Errorf("再次切回译文应直接显示缓存，文本 %v", tt.Texts())
	}

	// 换选 beta：翻一次；换回 alpha：命中缓存。
	click(t, a, tt, "beta")
	a.settle()
	tt.Frame()
	if f.calls != 2 {
		t.Errorf("换选新技能应再翻一次，calls=%d texts=%v", f.calls, f.texts)
	}
	click(t, a, tt, "alpha")
	a.settle()
	tt.Frame()
	if f.calls != 2 {
		t.Errorf("换回已翻过的技能应命中缓存，calls=%d", f.calls)
	}
	if countText(tt, "[译]Reads PDF files.") != 2 {
		t.Errorf("换回后应显示 alpha 的译文，文本 %v", tt.Texts())
	}
}

// TestTranslateFailureKeepsOriginal 验证：翻译失败时界面显示错误、原文仍在、
// 不 panic；失败不会在每帧里自动重试，切回原文再切到译文才重试。
func TestTranslateFailureKeepsOriginal(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	f.err = errors.New("store: 翻译服务返回 401: invalid api key")
	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	for i := 0; i < 3; i++ {
		tt.Frame()
		a.settle()
	}

	if !detHasText(tt, "翻译失败：") || !detHasText(tt, "401") {
		t.Errorf("应显示翻译失败与原因，文本 %v", tt.Texts())
	}
	if countText(tt, "Reads PDF files.") != 2 {
		t.Errorf("失败时详情与卡片仍应显示原文，文本 %v", tt.Texts())
	}
	if f.calls != 1 {
		t.Errorf("失败后不应在每帧里重试，calls=%d", f.calls)
	}

	// 服务恢复后，切回原文再切到译文即重试。
	f.err = nil
	click(t, a, tt, "原文")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	if f.calls != 2 {
		t.Errorf("重新切到译文应重试，calls=%d", f.calls)
	}
	if countText(tt, "[译]Reads PDF files.") != 2 {
		t.Errorf("重试成功后应显示译文，文本 %v", tt.Texts())
	}
}

// TestTranslateErrorNeverLeaksKey 验证：假翻译返回的错误里即使带了密钥，
// 界面上也看不到它。
func TestTranslateErrorNeverLeaksKey(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	f.err = errors.New("401 for Authorization: Bearer " + fakeKey)
	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	for _, s := range tt.Texts() {
		if strings.Contains(s, fakeKey) {
			t.Fatalf("界面文本泄露了密钥：%q", s)
		}
	}
	if !detHasText(tt, "翻译失败：") {
		t.Errorf("应显示翻译失败，文本 %v", tt.Texts())
	}
}

// TestTranslateDoesNotTouchSkillDir 验证：翻译全过程（含失败、换选、切换）
// 不对技能目录做任何写操作：目录树的文件名与字节翻译前后完全一致。
func TestTranslateDoesNotTouchSkillDir(t *testing.T) {
	a, cfg, f, tt := trApp(t, true)
	// 给 alpha 一份元数据，让 .kura-meta.json 也在比较范围内。
	data := []byte("{\n  \"metaVersion\": 1,\n  \"id\": \"alpha\"\n}\n")
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("alpha"), store.MetaFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// SKILL.md 放一句独一无二的正文：全文翻译失败时它仍须原样显示。
	body := "---\nname: alpha\ndescription: Reads PDF files.\n---\n\n# alpha\n\n这是原文段落，译文失败时要保留。\n"
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.settle()
	tt.Frame()

	before := snapshotTree(t, cfg.Root)
	if len(before) < 3 {
		t.Fatalf("快照应覆盖 SKILL.md 与元数据，得到 %v", before)
	}

	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	click(t, a, tt, "beta")
	a.settle()
	tt.Frame()
	f.err = errors.New("boom")
	click(t, a, tt, "原文")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	click(t, a, tt, "alpha")
	detClickTab(t, a, tt, detailTabFull) // 译文模式下打开全文：翻译失败，说明原因并保留原文
	if !detHasText(tt, "全文翻译失败") {
		t.Errorf("全文译文失败时应说明原因，文本 %v", tt.Texts())
	}
	if !detHasText(tt, "这是原文段落") {
		t.Errorf("全文译文失败时应仍显示原文，文本 %v", tt.Texts())
	}
	if f.calls == 0 {
		t.Fatal("测试没有真的触发翻译，断言无意义")
	}

	after := snapshotTree(t, cfg.Root)
	if len(after) != len(before) {
		t.Fatalf("技能目录文件数变了：前 %d 后 %d", len(before), len(after))
	}
	for p, b := range before {
		if after[p] != b {
			t.Errorf("技能目录文件 %s 在翻译前后不一致", p)
		}
	}
}

// snapshotTree 读出 root 下所有文件的 相对路径 → 字节 映射。
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTranslateSettingsChangeInvalidatesCache 验证：保存了不同的翻译配置
// （这里是换目标语言）后，旧译文作废并按新配置重新翻译；在途的旧结果被丢弃。
func TestTranslateSettingsChangeInvalidatesCache(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	if f.calls != 1 {
		t.Fatalf("前置：应已翻译一次，calls=%d", f.calls)
	}

	s := a.settings
	s.TargetLang = "ja"
	a.SaveSettingsAsync(s)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()
	if f.calls != 2 {
		t.Errorf("换目标语言后应重新翻译，calls=%d", f.calls)
	}
	if !detHasText(tt, "译文 · 日本語") {
		t.Errorf("详情应标明新的目标语言，文本 %v", tt.Texts())
	}

	// 只保存同样的设置（没变）：不清缓存、不重翻。
	a.SaveSettingsAsync(a.settings)
	a.settle()
	tt.Frame()
	if f.calls != 2 {
		t.Errorf("设置没变不应重翻，calls=%d", f.calls)
	}
}

// TestTranslateStaleResultDropped 验证：请求在途时配置变了（epoch 递增），
// 迟到的旧结果不写进新缓存。
func TestTranslateStaleResultDropped(t *testing.T) {
	a, _, _, _ := trApp(t, true)
	a.TranslateAsync("detail:alpha:description", "Reads PDF files.")
	// 此时结果还在 pending 队列里（update 未回放）：配置变更清缓存。
	a.trans.reset()
	a.settle()
	if _, ok := a.trans.lookup("detail:alpha:description"); ok {
		t.Error("配置变更后迟到的旧结果不应写进缓存")
	}
	if len(a.trans.items) != 0 {
		t.Errorf("缓存应为空，得到 %v", a.trans.items)
	}
}

// TestTranslateStateMachine 直接测 translateState：占位、完成、失败、
// 原文变化与回退。
func TestTranslateStateMachine(t *testing.T) {
	s := newTranslateState()
	const key, src = "detail:x:description", "hello"

	if got := s.text(key, src); got != src {
		t.Errorf("无缓存时应返回原文，得到 %q", got)
	}
	if !s.begin(key, src) {
		t.Fatal("首次 begin 应成功")
	}
	if s.begin(key, src) {
		t.Error("进行中再次 begin 应返回 false（同一 key 只发一次）")
	}
	if got := s.text(key, src); got != src {
		t.Errorf("进行中应显示原文，得到 %q", got)
	}
	s.finish(key, src, "你好", "")
	if dst, ok := s.lookup(key); !ok || dst != "你好" {
		t.Errorf("完成后 lookup = %q,%v", dst, ok)
	}
	if got := s.text(key, src); got != "你好" {
		t.Errorf("完成后应显示译文，得到 %q", got)
	}
	if s.begin(key, src) {
		t.Error("已完成再 begin 应返回 false")
	}
	// 原文变了：旧译文不冒充新原文的译文，且允许重新翻译。
	if got := s.text(key, "hello world"); got != "hello world" {
		t.Errorf("原文变化后应返回新原文，得到 %q", got)
	}
	if !s.begin(key, "hello world") {
		t.Error("原文变化后应允许重新翻译")
	}
	// 失败：记录错误、清掉译文；clearErrors 后可重试。
	s.finish(key, "hello world", "", "boom")
	if e := s.items[key]; e.err != "boom" || e.dst != "" || e.pending {
		t.Errorf("失败条目应只有 err：%+v", e)
	}
	if s.begin(key, "hello world") {
		t.Error("失败后不应自动重试")
	}
	s.clearErrors()
	if !s.begin(key, "hello world") {
		t.Error("clearErrors 后应可重试")
	}
}
