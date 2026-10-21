package selfupdate

// host.go — Background 与 MyGo 更新器之间的全部接触面。生产实现（productionHost）
// 转发给 updater 包与 mygo.Updater；测试注入假实现（见 watch_test.go），绝不挂
// 插件、绝不联网。
//
// 插件有两项状态没有公开的读取函数：用户在更新窗口里「跳过这个版本」记下的版本号，
// 以及插件自己存的自动检查开关。它们只能从插件的 updater.json 读。这个文件是插件
// 文档化的用户数据（用户数据目录下的 updater.json），每次保存都整体重写，读到的值与
// 插件内存里的一致；这里只读几个字段，不写，也不改插件。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// pluginStateFile 是插件在用户数据目录里的状态文件名。
const pluginStateFile = "updater.json"

type host interface {
	// enabled 报告应用能否自我更新（打包构建、带更新源与公钥、能写安装目录）。
	// 为假时 kura 完全不做后台检查，插件也不调度。
	enabled() bool
	// check 向更新源问一次：有新版本时返回它的版本号与 available 为真。
	check(ctx context.Context) (version string, available bool, err error)
	// automaticDownloads 是插件里「以后自动下载并安装更新」的勾选。
	automaticDownloads() bool
	// lastCheck 是插件最近一次成功检查的时间（用户手动检查也算）。
	lastCheck() time.Time
	// appVersion 是当前运行的应用版本（打包构建才有；开发构建与 go test 里是空串）。
	// 用来判断 config.json 里记的检查时间属于哪个版本：装完新版本后旧的时间不再
	// 推迟后台检查。它不涉及插件，与上面几项放一起是为了让测试能注入。
	appVersion() string
	// skippedVersion 是用户在更新窗口里选「跳过这个版本」记下的版本号，没有则为空串。
	skippedVersion() string
	// legacyAutomaticChecks 是插件 updater.json 里 automaticChecks 的当前值，nil 表示
	// 从未设过。只用于把旧版本的偏好迁移进 config.json，见 store.ResolveAutoCheck。
	legacyAutomaticChecks() *bool
	// setAutomaticChecks 写插件的调度开关：开着，插件才会按自己的节奏后台检查并静默安装。
	setAutomaticChecks(on bool)
	// onChange 注册插件偏好或检查时间变化时的回调（主线程），不再取消。
	onChange(fn func())
	// onResume 注册系统唤醒的回调，不再取消。
	onResume(fn func())
}

// productionHost 是 host 的生产实现，只做转发。
type productionHost struct{}

func (productionHost) enabled() bool { return mygo.Updater.Enabled() }

func (productionHost) check(ctx context.Context) (string, bool, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return "", false, err
	}
	return up.Version, true, nil
}

func (productionHost) automaticDownloads() bool { return updater.AutomaticDownloads() }

func (productionHost) lastCheck() time.Time { return updater.LastCheck() }

// appVersion 转发给包的 appVersion（同一份：mygo.json 的 version 是应用版本号的唯一来源，
// 这里不另记一份；「关于」弹窗的版本行也用它）。开发构建与 go test 里它可能为空串，调用方
// 按「版本未知」处理。
func (productionHost) appVersion() string { return appVersion() }

func (productionHost) skippedVersion() string { return readPluginFile().SkippedVersion }

func (productionHost) legacyAutomaticChecks() *bool { return readPluginFile().AutomaticChecks }

func (productionHost) setAutomaticChecks(on bool) { updater.SetAutomaticChecks(on) }

func (productionHost) onChange(fn func()) { updater.OnChange(fn) }

func (productionHost) onResume(fn func()) { mygo.Power.OnResume(fn) }

// pluginFile 是 updater.json 里 Background 关心的字段（插件的状态还有别的，不读）。
type pluginFile struct {
	AutomaticChecks *bool  `json:"automaticChecks"`
	SkippedVersion  string `json:"skippedVersion"`
}

// readPluginFile 读 updater.json。文件不存在时返回零值。
func readPluginFile() pluginFile {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return pluginFile{}
	}
	data, err := os.ReadFile(filepath.Join(dir, pluginStateFile))
	if err != nil {
		return pluginFile{}
	}
	return parsePluginFile(data)
}

// parsePluginFile 解析 updater.json。损坏的文件当成没有，与插件的处理一致。
func parsePluginFile(data []byte) pluginFile {
	var f pluginFile
	_ = json.Unmarshal(data, &f)
	return f
}
