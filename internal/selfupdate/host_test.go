package selfupdate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// TestParsePluginFileReadsThePluginsKeys 验证读取的字段名与插件写入 updater.json 的一致
// （字段名见 mygo 的 plugins/updater 的 state 结构）；损坏或缺省的文件当成没有。
func TestParsePluginFileReadsThePluginsKeys(t *testing.T) {
	f := parsePluginFile([]byte(`{"automaticChecks":false,"automaticDownloads":true,` +
		`"skippedVersion":"0.2.0","lastCheck":"2026-10-18T09:00:00Z"}`))
	if f.AutomaticChecks == nil || *f.AutomaticChecks {
		t.Errorf("automaticChecks 应读成假，得到 %v", f.AutomaticChecks)
	}
	if f.SkippedVersion != "0.2.0" {
		t.Errorf("skippedVersion = %q，想要 0.2.0", f.SkippedVersion)
	}

	for name, data := range map[string][]byte{
		"空文件":  nil,
		"损坏":   []byte(`{"automaticChecks":`),
		"只有时间": []byte(`{"lastCheck":"2026-10-18T09:00:00Z"}`),
	} {
		got := parsePluginFile(data)
		if got.AutomaticChecks != nil || got.SkippedVersion != "" {
			t.Errorf("%s：应当当成没有设过，得到 %+v", name, got)
		}
	}
}

// TestReadPluginFileReadsWhereThePluginWritesIt 验证读取的位置与插件一致：插件的 stateDir
// 就是 mygo.App.Path(PathUserData)。用 SetPath 把它换成临时目录，不碰真实的用户配置目录。
func TestReadPluginFileReadsWhereThePluginWritesIt(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	body := `{"automaticChecks":false,"skippedVersion":"0.2.0"}`
	if err := os.WriteFile(filepath.Join(dir, pluginStateFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f := readPluginFile()
	if f.AutomaticChecks == nil || *f.AutomaticChecks || f.SkippedVersion != "0.2.0" {
		t.Errorf("readPluginFile() = %+v，想要 automaticChecks 为假、skippedVersion 为 0.2.0", f)
	}
}
