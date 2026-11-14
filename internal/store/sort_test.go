package store

// 排序偏好（~/.kura/config.json 的 sort）的测试：规范化、默认值、读取（含手改出的
// 损坏值）、写入与其他偏好互不覆盖、config.json 损坏时的覆盖、并发写。
//
// 默认值一律用字面量断言（{"key":"time","desc":true}），不拿 DefaultSort() 自己跟自己
// 比：否则有人把默认悄悄改成按名称时，这些用例会跟着一起改、什么也拦不住。
// 目录都在 t.TempDir() 里，绝不触碰真实的 ~/.kura。

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
)

// 摆手改出的各种 config.json 内容用 autocheck_test.go 里现成的 writeRawConfig。

// diskTopLevel 把 config.json 读成「顶层键 → 原始 JSON」，便于检查某一项在盘上的真实形状。
func diskTopLevel(t *testing.T, base *Config) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(base.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("config.json 不是合法 JSON：%v\n%s", err, data)
	}
	return m
}

// TestDefaultSortIsNewestFirst 钉住默认值：按修改时间、新的在前。
func TestDefaultSortIsNewestFirst(t *testing.T) {
	if got, want := DefaultSort(), (SortPref{Key: "time", Desc: true}); got != want {
		t.Fatalf("DefaultSort() = %+v，想要 %+v", got, want)
	}
}

// TestNormalizeSort 验证规范化：空值是默认；名称的方向没有意义，规范成 false；时间保留方向；
// 不认识的 key（含大小写不对）返回 ErrUnknownSort。
func TestNormalizeSort(t *testing.T) {
	cases := []struct {
		name    string
		in      SortPref
		want    SortPref
		wantErr error
	}{
		{"零值是默认", SortPref{}, SortPref{Key: "time", Desc: true}, nil},
		{"空 key 带 desc=false 也是默认", SortPref{Desc: false}, SortPref{Key: "time", Desc: true}, nil},
		{"时间·新的在前", SortPref{Key: "time", Desc: true}, SortPref{Key: "time", Desc: true}, nil},
		{"时间·旧的在前", SortPref{Key: "time", Desc: false}, SortPref{Key: "time", Desc: false}, nil},
		{"名称", SortPref{Key: "name"}, SortPref{Key: "name"}, nil},
		{"名称的 desc 没有意义，规范成 false", SortPref{Key: "name", Desc: true}, SortPref{Key: "name"}, nil},
		{"不认识的 key", SortPref{Key: "size"}, SortPref{}, ErrUnknownSort},
		{"大小写要对", SortPref{Key: "TIME"}, SortPref{}, ErrUnknownSort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSort(tc.in)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Errorf("NormalizeSort(%+v) = %+v, %v；想要 %+v, %v", tc.in, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// TestLoadLibrarySortDefaultsToNewestFirst 验证没存过排序时读到默认（按修改时间、新的在前）：
// 既包括没有 config.json，也包括有 config.json 但没有 sort 一项。
func TestLoadLibrarySortDefaultsToNewestFirst(t *testing.T) {
	want := SortPref{Key: "time", Desc: true}
	base, extra := newLibraryFixture(t)
	if lib, err := LoadLibrary(base); err != nil || lib.Sort != want {
		t.Fatalf("没有 config.json：Sort = %+v, %v，想要 %+v", lib.Sort, err, want)
	}
	writeRawConfig(t, base, `{"roots":[`+jsonString(extra)+`],"theme":"dark"}`)
	lib, err := LoadLibrary(base)
	if err != nil || lib.Sort != want {
		t.Fatalf("config.json 里没有 sort：Sort = %+v, %v，想要 %+v", lib.Sort, err, want)
	}
	if lib.Theme != ThemeDark || len(lib.Roots) != 2 {
		t.Errorf("其他内容照常读：Theme=%q Roots=%v", lib.Theme, lib.Roots)
	}
}

// jsonString 把 s 编码成 JSON 字符串字面量（Windows 路径里的反斜杠要转义）。
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestLoadLibraryReadsStoredSort 验证存下的排序读得回：四种合法取值，以及 desc 缺省为真、
// 名称的 desc 被规范掉。
func TestLoadLibraryReadsStoredSort(t *testing.T) {
	cases := []struct {
		raw  string // sort 一项在 config.json 里的写法
		want SortPref
	}{
		{`{"key":"name"}`, SortPref{Key: "name"}},
		{`{"key":"name","desc":true}`, SortPref{Key: "name"}},
		{`{"key":"name","desc":false}`, SortPref{Key: "name"}},
		{`{"key":"time","desc":true}`, SortPref{Key: "time", Desc: true}},
		{`{"key":"time","desc":false}`, SortPref{Key: "time", Desc: false}},
		{`{"key":"time"}`, SortPref{Key: "time", Desc: true}}, // desc 缺省为真
		// 多余字段只是被忽略，合法的 key 仍然生效。
		{`{"key":"name","extra":[1,2,3]}`, SortPref{Key: "name"}},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			base, _ := newLibraryFixture(t)
			writeRawConfig(t, base, `{"roots":[],"sort":`+tc.raw+`}`)
			lib, err := LoadLibrary(base)
			if err != nil || lib.Sort != tc.want {
				t.Errorf("sort=%s：Sort = %+v, %v，想要 %+v", tc.raw, lib.Sort, err, tc.want)
			}
		})
	}
}

// TestLoadLibraryTreatsBrokenSortAsDefault 验证手改出的损坏/未知的 sort 一律退回默认，且
// 不当作错误：config.json 其余内容（根目录列表、外观）照常读出，不会因为 sort 类型不对而
// 把整个文件当成损坏。
func TestLoadLibraryTreatsBrokenSortAsDefault(t *testing.T) {
	want := SortPref{Key: "time", Desc: true}
	for _, raw := range []string{
		`"time"`,                      // 写成了字符串
		`5`,                           // 写成了数字
		`null`,                        // 显式 null
		`[]`,                          // 写成了数组
		`{}`,                          // 空对象
		`{"key":5}`,                   // key 类型不对
		`{"key":"weird"}`,             // 不认识的 key
		`{"key":"TIME"}`,              // 大小写不对
		`{"key":"time","desc":"yes"}`, // desc 类型不对
		`{"desc":false}`,              // 缺 key：整体按默认
	} {
		t.Run(raw, func(t *testing.T) {
			base, extra := newLibraryFixture(t)
			writeRawConfig(t, base, `{"roots":[`+jsonString(extra)+`],"theme":"dark","sort":`+raw+`}`)
			lib, err := LoadLibrary(base)
			if err != nil {
				t.Fatalf("损坏的 sort 不该让 config.json 读取报错：%v", err)
			}
			if lib.Sort != want {
				t.Errorf("sort=%s：Sort = %+v，想要默认 %+v", raw, lib.Sort, want)
			}
			if lib.Theme != ThemeDark || len(lib.Roots) != 2 || lib.Roots[1] != extra {
				t.Errorf("sort 损坏时其余内容应照常读出：Theme=%q Roots=%v", lib.Theme, lib.Roots)
			}
		})
	}
}

// TestSetSortPersistsAndKeepsOtherPrefs 验证 SetSort 写盘后重新加载读得回，在盘上是约定的
// 形状 {"key":..,"desc":..}，且不抹掉已有的外观、排列与根目录列表；反过来之后改外观、排列、
// 根目录也不抹掉排序。
func TestSetSortPersistsAndKeepsOtherPrefs(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetTheme(ThemeDark); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetView(ViewList); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetSort(SortPref{Key: SortTime, Desc: false}); err != nil {
		t.Fatal(err)
	}
	if lib.Sort != (SortPref{Key: "time", Desc: false}) {
		t.Errorf("SetSort 成功后内存里的 Sort = %+v", lib.Sort)
	}

	// 盘上的形状：sort 是 {"key":"time","desc":false}，desc=false 要明确写出来（缺省是真）。
	var onDisk struct {
		Key  string `json:"key"`
		Desc *bool  `json:"desc"`
	}
	if err := json.Unmarshal(diskTopLevel(t, base)["sort"], &onDisk); err != nil {
		t.Fatalf("盘上的 sort 不是对象：%v", err)
	}
	if onDisk.Key != "time" || onDisk.Desc == nil || *onDisk.Desc {
		t.Errorf("盘上的 sort = %+v，想要 key=time desc=false（desc 要明确写出）", onDisk)
	}

	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if again.Sort != (SortPref{Key: "time", Desc: false}) {
		t.Errorf("重新加载后 Sort = %+v，想要 time/旧的在前", again.Sort)
	}
	if again.Theme != ThemeDark || again.View != ViewList || len(again.Roots) != 2 || again.Roots[1] != extra {
		t.Errorf("SetSort 不应抹掉其他偏好：Theme=%q View=%q Roots=%v", again.Theme, again.View, again.Roots)
	}

	// 反过来：改外观、排列、根目录，都不抹掉排序。
	if err := again.SetTheme(ThemeLight); err != nil {
		t.Fatal(err)
	}
	if err := again.SetView(ViewGrid); err != nil {
		t.Fatal(err)
	}
	if err := again.RemoveRoot(extra); err != nil {
		t.Fatal(err)
	}
	last, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if last.Sort != (SortPref{Key: "time", Desc: false}) || last.Theme != ThemeLight || last.View != ViewGrid {
		t.Errorf("改其他偏好后 Sort=%+v Theme=%q View=%q，想要 time/旧的在前 light grid", last.Sort, last.Theme, last.View)
	}

	// 切到名称，读回来 desc 已规范成 false。
	if err := last.SetSort(SortPref{Key: SortName, Desc: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadLibrary(base); got.Sort != (SortPref{Key: "name"}) {
		t.Errorf("切到名称后 Sort = %+v，想要 name", got.Sort)
	}
	// 零值写成默认，且是明确写出来的。
	if err := last.SetSort(SortPref{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadLibrary(base); got.Sort != (SortPref{Key: "time", Desc: true}) {
		t.Errorf("SetSort(零值) 后 Sort = %+v，想要默认", got.Sort)
	}
}

// TestSetSortRejectsUnknownKeyWithoutWriting 验证不认识的 key 被拒绝，并且不写盘。
func TestSetSortRejectsUnknownKeyWithoutWriting(t *testing.T) {
	base, _ := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.SetTheme(ThemeLight); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(base.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetSort(SortPref{Key: "size"}); !errors.Is(err, ErrUnknownSort) {
		t.Fatalf("SetSort(size) = %v，想要 ErrUnknownSort", err)
	}
	after, _ := os.ReadFile(base.ConfigPath())
	if string(before) != string(after) {
		t.Errorf("被拒绝的值不应写盘：\n前 %s\n后 %s", before, after)
	}
	if lib.Sort != (SortPref{Key: "time", Desc: true}) {
		t.Errorf("被拒绝后内存里的 Sort 不应变，得到 %+v", lib.Sort)
	}
}

// TestUnrelatedWritesKeepAHandEditedSortValue 验证「其余内容原样保留」：手写成不认识的
// sort 值时，改外观这类无关写入不会替用户改掉它（读取时仍按默认），而 SetSort 才会把它
// 换成合法的值。
func TestUnrelatedWritesKeepAHandEditedSortValue(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, `{"roots":[],"sort":"by-vibes"}`)
	lib, err := LoadLibrary(base)
	if err != nil || lib.Sort != (SortPref{Key: "time", Desc: true}) {
		t.Fatalf("读取：Sort = %+v, %v，想要默认且无错误", lib.Sort, err)
	}
	if err := lib.SetTheme(ThemeDark); err != nil {
		t.Fatal(err)
	}
	if got := string(diskTopLevel(t, base)["sort"]); got != `"by-vibes"` {
		t.Errorf("无关写入后盘上的 sort = %s，想要原样保留 \"by-vibes\"", got)
	}
	if err := lib.SetSort(SortPref{Key: SortName}); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadLibrary(base); err != nil || got.Sort != (SortPref{Key: "name"}) {
		t.Errorf("SetSort 之后 Sort = %+v, %v，想要 name", got.Sort, err)
	}
}

// TestSetSortOverwritesCorruptConfigAndCarriesPrefs 验证 config.json 损坏（不是合法 JSON）
// 时 SetSort 仍能写下偏好（与 SetTheme / SetView 一样覆盖坏文件），并且本次运行里已定下的
// 其他偏好随内存副本带过去；而内存里是默认排序时，覆盖后的文件里不写 sort 一项。
func TestSetSortOverwritesCorruptConfigAndCarriesPrefs(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, "{bad")
	lib, err := LoadLibrary(base)
	if err == nil {
		t.Fatal("损坏的 config.json 应报错（但库仍可用）")
	}
	lib.Theme, lib.View = ThemeDark, ViewList // 本次运行里定下的偏好
	if err := lib.SetSort(SortPref{Key: SortName}); err != nil {
		t.Fatalf("损坏后仍应能保存排序：%v", err)
	}
	got, err := LoadLibrary(base)
	if err != nil || got.Sort != (SortPref{Key: "name"}) || got.Theme != ThemeDark || got.View != ViewList || got.Default() != base.Root {
		t.Errorf("覆盖后重新读取得到 %+v, %v", got, err)
	}

	// 内存里是默认排序：别的写入覆盖坏文件时不写 sort 一项（读取时本来就按默认）。
	writeRawConfig(t, base, "{bad again")
	fresh, _ := LoadLibrary(base)
	if err := fresh.SetTheme(ThemeLight); err != nil {
		t.Fatal(err)
	}
	if _, ok := diskTopLevel(t, base)["sort"]; ok {
		t.Error("默认排序不该被写进覆盖后的 config.json")
	}
	// 内存里是非默认排序：随副本带过去。
	writeRawConfig(t, base, "{bad third")
	carried, _ := LoadLibrary(base)
	carried.Sort = SortPref{Key: SortTime, Desc: false}
	if err := carried.SetView(ViewList); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadLibrary(base); err != nil || got.Sort != (SortPref{Key: "time", Desc: false}) {
		t.Errorf("非默认排序应随内存副本带过去：Sort = %+v, %v", got.Sort, err)
	}
}

// TestSortWriterDoesNotClobberOthers 验证排序、排列与目录列表这三个各自「读—改—写」
// config.json 的后台任务同时进行时，谁也不会丢掉别人的改动（SetSort 与其他写入共用 configMu）。
// 每一轮让三个任务同时起跑，轮末检查三边的改动都在；没有 configMu 时会很快失败（-race 下
// 也会被检出数据竞争）。
func TestSortWriterDoesNotClobberOthers(t *testing.T) {
	base, extra := newLibraryFixture(t)
	sortLib, _ := LoadLibrary(base)
	viewLib, _ := LoadLibrary(base)
	rootsLib, _ := LoadLibrary(base)
	for i := 0; i < 150; i++ {
		pref, add := SortPref{Key: SortTime, Desc: i%2 == 0}, i%2 == 0
		view := ViewGrid
		if i%2 == 1 {
			view = ViewList
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		run := func(fn func() error) {
			defer wg.Done()
			<-start
			if err := fn(); err != nil {
				t.Error(err)
			}
		}
		go run(func() error { return sortLib.SetSort(pref) })
		go run(func() error { return viewLib.SetView(view) })
		go run(func() error {
			if add {
				return rootsLib.AddRoot(extra)
			}
			return rootsLib.RemoveRoot(extra)
		})
		close(start)
		wg.Wait()

		got, err := LoadLibrary(base)
		wantRoots := 1
		if add {
			wantRoots = 2
		}
		if err != nil || got.Sort != pref || got.View != view || len(got.Roots) != wantRoots {
			t.Fatalf("第 %d 轮后 config.json 丢了改动：Sort=%+v View=%q Roots=%v err=%v，想要 %+v、%q 与 %d 个目录",
				i, got.Sort, got.View, got.Roots, err, pref, view, wantRoots)
		}
	}
}
