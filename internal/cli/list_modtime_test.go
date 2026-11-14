package cli

// list --json 的 modTime 字段：技能「最近动过」的时间，取值是 max(目录自身、SKILL.md、
// .kura-meta.json 三个 mtime)，与文件系统里的真实 mtime 一致（链接技能取目标的）。
// 命令行永远按名称输出技能：config.json 的 sort 只对界面生效，这是有意为之。
//
// mtime 一律用 os.Chtimes 在真实文件系统上造；全部目录都在 t.TempDir() 里，绝不触碰
// 真实的 ~/.agents/skills 与 ~/.kura。

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cliMTimeBase 是造 mtime 的基准（整秒，远早于「现在」）。
var cliMTimeBase = time.Date(2024, 3, 5, 6, 7, 8, 0, time.UTC)

// cliSetMTime 把 path 的 mtime 设成基准之后 hours 小时，返回设下的时刻。
func cliSetMTime(t *testing.T, path string, hours int) time.Time {
	t.Helper()
	tm := cliMTimeBase.Add(time.Duration(hours) * time.Hour)
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatalf("设置 mtime 失败 %s: %v", path, err)
	}
	return tm
}

// listJSON 运行 list --json，返回技能出现的顺序，以及「名字 -> 条目」。
func listJSON(t *testing.T, e *cliEnv) ([]string, map[string]map[string]any) {
	t.Helper()
	code, out := e.runJSON(t, "list")
	if code != ExitOK {
		t.Fatalf("list 应退出 0，得到 %d，stderr=%s", code, e.stderr.String())
	}
	var order []string
	byName := map[string]map[string]any{}
	for _, raw := range out["skills"].([]any) {
		s := raw.(map[string]any)
		name := s["name"].(string)
		order = append(order, name)
		byName[name] = s
	}
	return order, byName
}

// statMax 独立于被测代码，直接用 os.Stat 算出 max(目录, SKILL.md, 元数据) 的 mtime：
// 这是「文件系统里的真实值」，用来核对 CLI 输出而不是核对我们自己写下的期望常量。
func statMax(t *testing.T, dir string) time.Time {
	t.Helper()
	var latest time.Time
	for _, p := range []string{dir, filepath.Join(dir, "SKILL.md"), filepath.Join(dir, ".kura-meta.json")} {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

// parseModTime 取出条目的 modTime 并按 RFC 3339 解析；缺失或格式不对就失败。
func parseModTime(t *testing.T, s map[string]any) time.Time {
	t.Helper()
	raw, ok := s["modTime"].(string)
	if !ok || raw == "" {
		t.Fatalf("list --json 的条目缺少 modTime：%v", s)
	}
	if !strings.HasSuffix(raw, "Z") {
		t.Errorf("modTime 应是 UTC（以 Z 结尾），得到 %q", raw)
	}
	got, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("modTime %q 不是合法的 RFC 3339：%v", raw, err)
	}
	return got
}

// TestListJSONModTimeMatchesFilesystem 验证 list --json 每个技能都有 modTime，且与文件系统
// 里的真实 mtime 一致。四个技能各代表一种「谁最新」：只改了 SKILL.md、只增删了子条目（目录
// 最新）、刚更新过元数据、三者相同；时间的先后与名称顺序故意错开。停用、移入回收站后仍有。
func TestListJSONModTimeMatchesFilesystem(t *testing.T) {
	e := newEnv(t)
	type spec struct {
		name             string
		dirH, mdH, metaH int // 三个路径的 mtime（相对基准的小时数）；metaH<0 表示没有元数据
		wantH            int
	}
	specs := []spec{
		{"alpha", 10, 30, -1, 30},   // 只改了 SKILL.md 正文
		{"bravo", 40, 5, -1, 40},    // 只增删过子条目：目录最新
		{"charlie", 10, 20, 50, 50}, // 刚更新过元数据
		{"delta", 20, 20, 20, 20},   // 三者相同
	}
	for _, sp := range specs {
		e.writeSkill(sp.name)
		dir := filepath.Join(e.root, sp.name)
		if sp.metaH >= 0 {
			meta := filepath.Join(dir, ".kura-meta.json")
			if err := os.WriteFile(meta, []byte(`{"metaVersion":1}`), 0o644); err != nil {
				t.Fatal(err)
			}
			cliSetMTime(t, meta, sp.metaH)
		}
		cliSetMTime(t, filepath.Join(dir, "SKILL.md"), sp.mdH)
		cliSetMTime(t, dir, sp.dirH) // 目录最后设
	}

	order, byName := listJSON(t, e)
	if want := []string{"alpha", "bravo", "charlie", "delta"}; !slices.Equal(order, want) {
		t.Errorf("命令行永远按名称输出：得到 %v，想要 %v", order, want)
	}
	for _, sp := range specs {
		got := parseModTime(t, byName[sp.name])
		want := cliMTimeBase.Add(time.Duration(sp.wantH) * time.Hour)
		if !got.Equal(want) {
			t.Errorf("%s: modTime = %v，想要 %v", sp.name, got, want)
		}
		if real := statMax(t, filepath.Join(e.root, sp.name)); !got.Equal(real) {
			t.Errorf("%s: modTime = %v 与文件系统里的真实 mtime %v 不一致", sp.name, got, real)
		}
	}

	// 停用只是移动目录，modTime 不变；移入回收站后取的是容器的 mtime（刚发生）。
	if code := e.run("disable", "delta"); code != ExitOK {
		t.Fatalf("disable 失败：%s", e.stderr.String())
	}
	start := time.Now()
	if code := e.run("remove", "bravo"); code != ExitOK {
		t.Fatalf("remove 失败：%s", e.stderr.String())
	}
	_, byName = listJSON(t, e)
	if got, want := parseModTime(t, byName["delta"]), cliMTimeBase.Add(20*time.Hour); !got.Equal(want) || byName["delta"]["state"] != "disabled" {
		t.Errorf("停用后 delta: modTime = %v state=%v，想要不变的 %v 与 disabled", got, byName["delta"]["state"], want)
	}
	if got := parseModTime(t, byName["bravo"]); byName["bravo"]["state"] != "trashed" || got.Before(start.Add(-2*time.Second)) {
		t.Errorf("回收站里的 bravo: modTime = %v state=%v，想要约 %v 的移入时间", got, byName["bravo"]["state"], start.UTC())
	}
}

// TestListJSONModTimeFollowsLink 验证链接形式的技能（联接点 / 目录符号链接）输出的是
// 链接目标的 mtime，而不是链接自己的（它是测试刚建的，约等于「现在」）。
func TestListJSONModTimeFollowsLink(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		real := e.linkedSkill(t, mk, "linked")
		cliSetMTime(t, filepath.Join(real, "SKILL.md"), 11)
		want := cliSetMTime(t, filepath.Join(real, ".kura-meta.json"), 40)
		cliSetMTime(t, real, 20)

		_, byName := listJSON(t, e)
		got := parseModTime(t, byName["linked"])
		if !got.Equal(want) {
			t.Errorf("链接技能的 modTime = %v，想要目标里最大的 %v", got, want)
		}
		if real := statMax(t, real); !got.Equal(real) {
			t.Errorf("链接技能的 modTime = %v 与目标的真实 mtime %v 不一致", got, real)
		}
	})
}

// TestListOrderIgnoresSortPreference 验证 config.json 的 sort 对命令行没有任何影响：不论
// 存的是按时间新的在前、旧的在前、按名称，还是损坏的值，list 的顺序与内容都一样（按名称）。
// 排序只在界面里生效。技能的 mtime 与名称顺序故意相反，这样「按时间排了序」会被看出来。
func TestListOrderIgnoresSortPreference(t *testing.T) {
	e := newEnv(t)
	for i, name := range []string{"aaa", "bbb", "ccc"} {
		e.writeSkill(name)
		dir := filepath.Join(e.root, name)
		cliSetMTime(t, filepath.Join(dir, "SKILL.md"), 100-i*10) // aaa 最新，ccc 最旧
		cliSetMTime(t, dir, 100-i*10)
	}
	if err := os.MkdirAll(e.home, 0o755); err != nil {
		t.Fatal(err)
	}
	var first string
	for _, sortJSON := range []string{
		``, // 没有 sort
		`{"key":"time","desc":true}`,
		`{"key":"time","desc":false}`,
		`{"key":"name"}`,
		`"garbage"`,
	} {
		cfg := `{"roots":[]}`
		if sortJSON != "" {
			cfg = `{"roots":[],"sort":` + sortJSON + `}`
		}
		if err := os.WriteFile(filepath.Join(e.home, "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if code := e.run("--json", "list"); code != ExitOK {
			t.Fatalf("sort=%s: list 失败：%s", sortJSON, e.stderr.String())
		}
		got := e.stdout.String()
		if first == "" {
			first = got
		} else if got != first {
			t.Errorf("sort=%s 时 list 的输出与别的取值不同：\n%s\n---\n%s", sortJSON, first, got)
		}
		order, _ := listJSON(t, e)
		if want := []string{"aaa", "bbb", "ccc"}; !slices.Equal(order, want) {
			t.Errorf("sort=%s：命令行顺序 = %v，想要按名称 %v", sortJSON, order, want)
		}
	}
}
