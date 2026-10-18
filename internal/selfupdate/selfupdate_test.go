// selfupdate 包测试（里程碑 6）。
//
// 边界：更新器端到端流程（检查 → 下载 → 签名校验 → 替换 → 重启）需要打包并签名的发布
// 构建（git tag vX.Y.Z + MYGO_UPDATER_PRIVATE_KEY），本环境无法提供，属 UNVERIFIED
// （见 docs/note/updates.md）。这里只测能在普通 go test 里验证的部分：插件挂载、应用菜单
// 结构（含更新项与「先熄灭蓝点」的包装）、主窗口照常构建、关于弹窗用的版本号读取。
// 后台检查的编排见 watch_test.go，那里的 host 与时钟都是假的。
//
// 关键约束：updater.MenuItem() 与 App.Locale() 都要经 onMainValue 等待主线程事件循环，
// 测试进程里事件循环未启动，直接调用会永久阻塞——菜单类测试一律用替身条目注入 appMenu。
package selfupdate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	"github.com/ArnoChenFx/kura/locales"
	"github.com/egoist/mygo"
	myui "github.com/egoist/mygo/ui"
)

// TestMain 把界面语言固定成中文再跑本包测试：TestWindowStillBuilds 断言的是中文界面
// 文字（「全部检查」「检查更新」），不该随开发机或 CI 的系统 locale 变化（i18n 的默认值
// 是英文）。菜单类断言读目录（i18n.T），与语言无关。
func TestMain(m *testing.M) {
	i18n.Set(i18n.ZH)
	os.Exit(m.Run())
}

// TestInstallRegistersPlugin 冒烟：Install（挂 native 插件、关掉它的后台调度）在测试进程
// 里只做内存注册与 WhenReady 排队，不等待主线程，可以安全调用。mygo.Use 不允许同名插件
// 重复挂载，整个进程只能 Install 一次，因此本包只有这一个测试调它。
func TestInstallRegistersPlugin(t *testing.T) {
	Install()
}

// TestAppMenuStructure 验证应用菜单模板：顶层 kura/编辑/窗口三个菜单；kura 子菜单依次为
// 关于 role、关于 kura、分隔线、更新项、分隔线、退出。更新项用替身注入（生产环境此处是
// updater.MenuItem()，见 AppMenu）。菜单文字从目录取（i18n.T），所以断言与当前语言无关。
func TestAppMenuStructure(t *testing.T) {
	stub := &mygo.MenuItem{Label: "检查更新…"}
	m := appMenu(stub)
	items := m.Items()
	if len(items) != 3 {
		t.Fatalf("顶层应有 kura/编辑/窗口三个菜单，得到 %d", len(items))
	}
	if items[0].Label != appMenuLabel {
		t.Errorf("第一个菜单应叫 %q，得到 %q", appMenuLabel, items[0].Label)
	}
	if items[1].Role != mygo.RoleEditMenu || items[2].Role != mygo.RoleWindowMenu {
		t.Errorf("后两个菜单应为编辑/窗口 role，得到 %q / %q", items[1].Role, items[2].Role)
	}

	sub := items[0].Submenu
	if len(sub) != 6 { // 关于role + 关于kura + 分隔线 + 更新项 + 分隔线 + 退出
		t.Fatalf("kura 子菜单应有 6 项，得到 %d", len(sub))
	}
	if sub[0].Role != mygo.RoleAbout {
		t.Errorf("第 1 项应为 RoleAbout，得到 role=%q label=%q", sub[0].Role, sub[0].Label)
	}
	if sub[1].Label != locales.SelfupdateMenuAbout() || sub[1].Click == nil {
		t.Errorf("第 2 项应为 %q 且有 Click，得到 %+v", locales.SelfupdateMenuAbout(), sub[1])
	}
	if sub[2].Type != mygo.MenuItemSeparator || sub[4].Type != mygo.MenuItemSeparator {
		t.Errorf("第 3/5 项应为分隔线，得到 %q / %q", sub[2].Type, sub[4].Type)
	}
	if sub[3].Label != stub.Label || sub[3].Click != nil {
		t.Errorf("第 4 项应为注入的更新项（label=%q，替身无 Click），得到 %+v", stub.Label, sub[3])
	}
	if sub[5].Label != locales.SelfupdateMenuQuit() || sub[5].Role != mygo.RoleQuit {
		t.Errorf("第 6 项应为 %q（RoleQuit），得到 label=%q role=%q",
			locales.SelfupdateMenuQuit(), sub[5].Label, sub[5].Role)
	}
}

// TestAppMenuUpdateItemClick 验证注入的更新项 Click 原样保留——生产环境里它就是
// updater.MenuItem 的「打开更新窗口」回调。
func TestAppMenuUpdateItemClick(t *testing.T) {
	var clicked bool
	var mu sync.Mutex
	stub := &mygo.MenuItem{Label: "检查更新…", Click: func(*mygo.MenuItem, *mygo.Window) {
		mu.Lock()
		clicked = true
		mu.Unlock()
	}}
	sub := appMenu(stub).Items()[0].Submenu
	sub[3].Click(nil, nil)
	mu.Lock()
	defer mu.Unlock()
	if !clicked {
		t.Error("更新项的 Click 应被原样保留并可触发")
	}
}

// TestOpeningWithRunsBeforeClick 验证菜单的「检查更新…」先熄灭左栏的蓝点、再打开更新窗口：
// onOpen 排在原有 Click 之前，两者各执行一次；没有 onOpen 时原有 Click 照常执行。
func TestOpeningWithRunsBeforeClick(t *testing.T) {
	var order []string
	item := &mygo.MenuItem{Label: "检查更新…", Click: func(*mygo.MenuItem, *mygo.Window) {
		order = append(order, "open-window")
	}}
	openingWith(item, func() { order = append(order, "clear-dot") })
	item.Click(nil, nil)
	if !slices.Equal(order, []string{"clear-dot", "open-window"}) {
		t.Errorf("执行顺序 = %v，想要先熄灭蓝点、再打开窗口", order)
	}

	var opened bool
	bare := openingWith(&mygo.MenuItem{Click: func(*mygo.MenuItem, *mygo.Window) { opened = true }}, nil)
	bare.Click(nil, nil)
	if !opened {
		t.Error("没有 onOpen 时原有 Click 也应执行")
	}
}

// TestAppVersionEmptyInDev 验证：开发构建（go test，未打包）下版本号为空串，「关于」弹窗
// 据此省略版本行；打包后由 mygo.json 的 version 提供（见 docs/note/updates.md）。
func TestAppVersionEmptyInDev(t *testing.T) {
	if v := appVersion(); v != "" {
		t.Logf("开发构建里 App.Version() = %q（非空说明环境异常）", v)
	}
}

// panicChildEnv 置上时，本测试作为子进程运行：逐个调用依赖插件的入口，确认它们都 panic；
// 有任何一个没 panic 就以非 0 退出。
const panicChildEnv = "KURA_SELFUPDATE_PANIC_CHILD"

// forwards 是依赖插件的几个入口的调用方式（与生产调用点一一对应）。
func forwards() map[string]func() {
	return map[string]func(){
		"CheckForUpdates": CheckForUpdates,
		"AppMenu":         func() { AppMenu(nil) },
	}
}

// panicked 报告 fn 是否 panic（恢复后不放出去）。
func panicked(fn func()) (yes bool) {
	defer func() { yes = recover() != nil }()
	fn()
	return false
}

// TestForwardsPanicWithoutPlugin 验证依赖插件的入口在插件未挂载时都会 panic：界面绝不直接
// 调它们，只由 main.go 在 Install 之后接线。父进程跑子进程，子进程里没有 Install。
func TestForwardsPanicWithoutPlugin(t *testing.T) {
	if os.Getenv(panicChildEnv) == "1" {
		for name, fn := range forwards() {
			if !panicked(fn) {
				fmt.Fprintf(os.Stderr, "%s 在插件未挂载时没有 panic\n", name)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestForwardsPanicWithoutPlugin$", "-test.v")
	cmd.Env = append(os.Environ(), panicChildEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("子进程里依赖插件的入口应全部 panic（它自己会用非 0 退出报告没 panic 的那个）：%v\n%s",
			err, stderr.String())
	}
}

// TestWindowStillBuilds 验证：挂上更新插件后，技能主窗口照常构建——Tester 驱动真实视图一帧，
// 工具栏/技能级「全部检查」在位；应用级更新入口只在左栏左下角与菜单里，窗口文案里不出现
// 第二个「检查更新」（与技能级文案消歧，见 docs/note/updates.md）。
func TestWindowStillBuilds(t *testing.T) {
	root := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), filepath.Join(root, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	app := kuraui.NewApp(cfg)
	tt := myui.NewTester(app.View, 980, 640)
	if tt == nil {
		t.Fatal("tester 不应为 nil")
	}
	tt.Frame()
	if !tt.HasText("全部检查") {
		t.Error("工具栏应有技能级「全部检查」按钮")
	}
	// 空技能根目录下，详情面板未选中任何技能：窗口里应有左栏左下角的应用级「检查更新」
	// 按钮（打开 MyGo 更新窗口）；技能级按钮只出现在选中技能的详情面板，此时不应出现。
	if !tt.HasText("检查更新") {
		t.Error("窗口应有左下角的应用级「检查更新」按钮")
	}
	if tt.HasText("详情:检查更新") {
		t.Error("未选中技能时不应出现技能级「检查更新」按钮")
	}
}
