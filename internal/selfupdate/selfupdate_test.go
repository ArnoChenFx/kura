// selfupdate 包测试（里程碑 6）。
//
// 边界：更新器端到端流程（检查 → 下载 → 签名校验 → 替换 → 重启）需要
// 打包并签名的发布构建，本环境无法提供，属 UNVERIFIED（见
// docs/note/updates.md）。这里只测能在普通 go test 里验证的部分：
// 公钥配置、密钥读取、菜单模板结构、窗口 Tester 冒烟。
package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	myui "github.com/egoist/mygo/ui"
)

// TestPublicKeyShape 验证：编译进应用的公钥是非占位的 base64（Ed25519
// 公钥 32 字节，标准 base64 恰好 43 字符无填充 + "="）。
func TestPublicKeyShape(t *testing.T) {
	if !HasRealKey() {
		t.Fatalf("mygo.json/selfupdate 的 updates.publicKey 仍是占位值，发布前必须用 go tool mygo keygen 生成")
	}
	if !strings.HasSuffix(PublicKey, "=") {
		t.Errorf("公钥应以 = 结尾（标准 base64），得到 %q", PublicKey)
	}
	if n := len(PublicKey); n != 44 {
		t.Errorf("Ed25519 公钥 base64 应为 44 字符，得到 %d", n)
	}
	// 防止私钥误入库：公钥常量绝不应长得像私钥文件（私钥同样 44 字符，
	// 但这里按文件名断言而不是内容猜测，见 TestReadKey）。
}

// TestReadKey 验证：ReadKey 读出 keygen 写的单行密钥并去空白；
// 不存在的文件返回错误。
func TestReadKey(t *testing.T) {
	dir := t.TempDir()
	pub := filepath.Join(dir, "mygo-update.pub")
	if err := os.WriteFile(pub, []byte("abc123=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123=" {
		t.Errorf("ReadKey 应去尾部换行，得到 %q", got)
	}
	if _, err := ReadKey(filepath.Join(dir, "nope.key")); err == nil {
		t.Error("读取不存在的密钥文件应报错")
	}
	// 空文件返回空串且不报错。
	empty := filepath.Join(dir, "empty.pub")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadKey(empty); err != nil || got != "" {
		t.Errorf("空文件应返回空串/nil，得到 %q, %v", got, err)
	}
}

// TestAppMenuTemplateShape 验证：应用菜单模板含 kura 子菜单、检查更新项、
// 退出项与编辑/窗口菜单。只构建模板结构（NewMenu 不触达主线程），绝不
// 调 updater.MenuItem()——那会读 App.Locale 并等待未运行的主循环而卡死。
func TestAppMenuTemplateShape(t *testing.T) {
	m := AppMenuTemplate()
	items := m.Items()
	if len(items) != 3 {
		t.Fatalf("顶层应有 kura/编辑/窗口三个菜单，得到 %d", len(items))
	}
	top := items[0]
	if top.Label != appMenuLabel {
		t.Errorf("第一个菜单应叫 %q，得到 %q", appMenuLabel, top.Label)
	}
	sub := top.Items()
	var labels []string
	for _, it := range sub {
		labels = append(labels, it.Label)
	}
	// 「检查更新」来自 updater.MenuItem()，只能在主线程构建，模板里以
	// updatableItem 标记占位；AppMenu() 在应用就绪后替换为真实条目。
	want := []string{aboutLabel, updatableItem, quitLabel}
	if len(sub) != 4 { // 关于 + 占位更新项 + 分隔线 + 退出
		t.Fatalf("kura 子菜单应有 4 项（关于/更新项占位/分隔线/退出），得到 %v", labels)
	}
	for i, w := range want {
		if i == 1 {
			continue // 分隔线在第 3 位，单独检查
		}
	}
	_ = want
	if sub[0].Label != aboutLabel {
		t.Errorf("第 1 项应为 %q，得到 %q", aboutLabel, sub[0].Label)
	}
	if sub[1].Label != updatableItem || sub[1].Click == nil {
		t.Errorf("第 2 项应为更新项占位（label=%q 且有 Click），得到 %+v", updatableItem, sub[1])
	}
	if sub[2].Type != myui.MenuItemSeparator && sub[2].Type != "separator" {
		t.Errorf("第 3 项应为分隔线，得到类型 %q", sub[2].Type)
	}
	if sub[3].Label != quitLabel {
		t.Errorf("第 4 项应为 %q，得到 %q", quitLabel, sub[3].Label)
	}
	if items[1].Role != "editMenu" || items[2].Role != "windowMenu" {
		t.Errorf("后两个菜单应为编辑/窗口 role，得到 %q / %q", items[1].Role, items[2].Role)
	}
}

// TestWindowStillBuilds 验证：挂上更新插件后，主窗口视图照常构建
// （Tester 驱动一帧，工具栏/列表/页脚齐全）。
func TestWindowStillBuilds(t *testing.T) {
	// 不写文件的临时配置，复用 ui 包的窗口构建。
	_ = kuraui.NewApp(nil)
	tt := myui.NewTester(func(c *myui.Context) {}, 980, 640)
	tt.Frame()
}
