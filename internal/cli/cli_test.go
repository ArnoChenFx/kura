package cli

// CLI 层的表驱动测试：全部针对 t.TempDir() 构造的临时 root/home 运行，
// 绝不触碰真实的 ~/.agents/skills 或 ~/.kura。每个用例断言退出码与
// （--json 时）输出形状。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// cliEnv 是一次 CLI 运行的隔离环境。
type cliEnv struct {
	t      *testing.T
	root   string // 技能根目录
	home   string // kura 数据目录
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// newEnv 建立临时环境（root/home 都在 t.TempDir() 下）。
func newEnv(t *testing.T) *cliEnv {
	t.Helper()
	base := t.TempDir()
	return &cliEnv{
		t:      t,
		root:   filepath.Join(base, "skills"),
		home:   filepath.Join(base, "kura-home"),
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}
}

// run 执行一次 CLI 调用（注入 --root/--home，保证绝不使用真实目录），
// 返回退出码。
func (e *cliEnv) run(args ...string) int {
	e.t.Helper()
	e.stdout.Reset()
	e.stderr.Reset()
	full := append([]string{"--root", e.root, "--home", e.home}, args...)
	return Run(full, e.stdout, e.stderr)
}

// runJSON 执行并解析 stdout 上的 JSON 输出到 map。
func (e *cliEnv) runJSON(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	code := e.run(append([]string{"--json"}, args...)...)
	var out map[string]any
	if err := json.Unmarshal(e.stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\nstdout=%s stderr=%s", err, e.stdout.String(), e.stderr.String())
	}
	return code, out
}

// writeSkill 建一个最小技能目录（SKILL.md）。
func (e *cliEnv) writeSkill(name string) {
	e.t.Helper()
	dir := filepath.Join(e.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	content := "---\nname: " + name + "\n---\n\n# " + name
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// installWithMeta 建一个带完整元数据的技能（contentHash 与内容一致），
// 模拟 `kura add` 的产物。
func (e *cliEnv) installWithMeta(t *testing.T, name string) {
	t.Helper()
	e.writeSkill(name)
	dir := filepath.Join(e.root, name)
	hash, err := store.HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &store.Meta{
		ID: name, DisplayName: name,
		Source:      store.Source{Type: store.SourceLocal, Path: dir},
		InstalledAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		ContentHash: hash,
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, store.MetaFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// requireString 断言 obj[key] 是字符串且等于 want。
func requireString(t *testing.T, obj map[string]any, key, want string) {
	t.Helper()
	got, ok := obj[key].(string)
	if !ok || got != want {
		t.Errorf("%s = %v, 期望 %q", key, obj[key], want)
	}
}

func TestHelpAndVersion(t *testing.T) {
	e := newEnv(t)
	if code := e.run("--help"); code != ExitOK || !strings.Contains(e.stdout.String(), "用法") {
		t.Errorf("--help 应输出用法且退出码 0, got %d: %s", code, e.stdout.String())
	}
	if code := e.run("version"); code != ExitOK || !strings.Contains(e.stdout.String(), "kura ") {
		t.Errorf("version 应输出版本, got %d: %s", code, e.stdout.String())
	}
	if code := e.run("no-such-command"); code != ExitUsage {
		t.Errorf("未知命令应返回退出码 2, got %d", code)
	}
}

func TestListEmpty(t *testing.T) {
	e := newEnv(t)
	code, out := e.runJSON(t, "list")
	if code != ExitOK {
		t.Fatalf("空根目录 list 应退出 0, got %d, stderr=%s", code, e.stderr.String())
	}
	requireString(t, out, "root", e.root)
	skills, ok := out["skills"].([]any)
	if !ok || len(skills) != 0 {
		t.Errorf("空根目录应返回空 skills 数组, got %v", out["skills"])
	}
}

func TestListStatesAndShape(t *testing.T) {
	e := newEnv(t)
	e.installWithMeta(t, "alpha")
	e.writeSkill("beta") // 无元数据
	if code := e.run("disable", "alpha"); code != ExitOK {
		t.Fatalf("disable 失败: %s", e.stderr.String())
	}
	code, out := e.runJSON(t, "list")
	if code != ExitOK {
		t.Fatalf("list 失败: %s", e.stderr.String())
	}
	skills, ok := out["skills"].([]any)
	if !ok || len(skills) != 2 {
		t.Fatalf("应有 2 条技能（含 disabled+无元数据）, got %v", out["skills"])
	}
	// 按名字找技能。
	byName := map[string]map[string]any{}
	for _, raw := range skills {
		s := raw.(map[string]any)
		byName[s["name"].(string)] = s
	}
	a := byName["alpha"]
	requireString(t, a, "state", "disabled")
	if _, ok := a["sourceType"]; !ok {
		t.Error("有元数据的技能应输出 sourceType")
	}
	if _, ok := a["updatedAt"]; !ok {
		t.Error("有元数据的技能应输出 updatedAt")
	}
	b := byName["beta"]
	requireString(t, b, "state", "enabled")
	if v, ok := b["sourceType"]; ok {
		t.Errorf("无元数据的技能不应有 sourceType, got %v", v)
	}
}

func TestDisableEnableCycle(t *testing.T) {
	e := newEnv(t)
	e.installWithMeta(t, "demo")

	// disable：JSON 形状 + 目录移动。
	code, out := e.runJSON(t, "disable", "demo")
	if code != ExitOK {
		t.Fatalf("disable 失败: %s", e.stderr.String())
	}
	requireString(t, out, "id", "demo")
	requireString(t, out, "state", "disabled")
	if _, err := os.Stat(filepath.Join(e.home, "disabled", "demo")); err != nil {
		t.Errorf("技能应已移入 disabled: %v", err)
	}

	// 对不存在的技能 disable：退出码 1 + {"error": ...}。
	e.stderr.Reset()
	if code := e.run("--json", "disable", "ghost"); code != ExitError {
		t.Errorf("disable ghost 应退出 1, got %d", code)
	}
	var errOut map[string]any
	if err := json.Unmarshal(e.stderr.Bytes(), &errOut); err != nil || errOut["error"] == nil {
		t.Errorf("--json 模式错误应输出 {\"error\":...}, got %s", e.stderr.String())
	}

	// enable：移回根目录。
	code, out = e.runJSON(t, "enable", "demo")
	if code != ExitOK {
		t.Fatalf("enable 失败: %s", e.stderr.String())
	}
	requireString(t, out, "state", "enabled")
	if _, err := os.Stat(filepath.Join(e.root, "demo", "SKILL.md")); err != nil {
		t.Errorf("技能应已移回根目录: %v", err)
	}
}

func TestRemoveRestoreWithStamp(t *testing.T) {
	e := newEnv(t)
	e.installWithMeta(t, "gone")

	// remove 带自定义 stamp。
	code, out := e.runJSON(t, "remove", "gone", "--stamp", "20260101-000000")
	if code != ExitOK {
		t.Fatalf("remove 失败: %s", e.stderr.String())
	}
	requireString(t, out, "state", "trashed")
	wantDir := filepath.Join(e.home, "trash", "gone", "20260101-000000")
	requireString(t, out, "dir", wantDir)
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("回收站目录结构应是 trash/<name>/<stamp>: %v", err)
	}
	if v, ok := out["snapshotDir"]; !ok || !strings.Contains(v.(string), "snapshots") {
		t.Errorf("remove 应产出快照目录, got %v", out["snapshotDir"])
	}

	// 非法 stamp 被拒绝（路径穿越防护）。
	e.writeSkill("x")
	if code := e.run("remove", "x", "--stamp", "../evil"); code != ExitError {
		t.Errorf("含路径分隔/穿越的 stamp 应被拒绝, got %d", code)
	}

	// restore（默认最新）。
	code, out = e.runJSON(t, "restore", "gone")
	if code != ExitOK {
		t.Fatalf("restore 失败: %s", e.stderr.String())
	}
	requireString(t, out, "state", "enabled")
	if _, err := os.Stat(filepath.Join(e.root, "gone", "SKILL.md")); err != nil {
		t.Errorf("恢复后 SKILL.md 应存在: %v", err)
	}
}

func TestAddLocal(t *testing.T) {
	e := newEnv(t)
	// 来源目录放在 root 之外（模拟任意来源）。
	base := t.TempDir()
	src := filepath.Join(base, "src-skill")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# src"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 普通安装：名字取目录名，写入元数据。
	code, out := e.runJSON(t, "add", "--local", src)
	if code != ExitOK {
		t.Fatalf("add --local 失败: %s", e.stderr.String())
	}
	requireString(t, out, "id", "src-skill")
	requireString(t, out, "state", "enabled")
	if _, err := os.Stat(filepath.Join(e.root, "src-skill", store.MetaFileName)); err != nil {
		t.Errorf("add 后应写入 .kura-meta.json: %v", err)
	}

	// --name 改名安装。
	src2 := filepath.Join(base, "another")
	os.MkdirAll(src2, 0o755)
	os.WriteFile(filepath.Join(src2, "SKILL.md"), []byte("# a"), 0o644)
	code, out = e.runJSON(t, "add", "--local", src2, "--name", "renamed")
	if code != ExitOK {
		t.Fatalf("add --name 失败: %s", e.stderr.String())
	}
	requireString(t, out, "id", "renamed")
	if _, err := os.Stat(filepath.Join(e.root, "renamed", "SKILL.md")); err != nil {
		t.Errorf("改名安装后目录应为 renamed: %v", err)
	}

	// 来源与重名冲突：退出码 1。
	if code := e.run("add", "--local", src); code != ExitError {
		t.Errorf("重名 add 应退出 1, got %d", code)
	}
	// 来源非法（无 SKILL.md）。
	bad := filepath.Join(base, "noskill")
	os.MkdirAll(bad, 0o755)
	if code := e.run("add", "--local", bad); code != ExitError {
		t.Errorf("缺 SKILL.md 的 add 应退出 1, got %d", code)
	}
}

func TestUpdateLocal(t *testing.T) {
	e := newEnv(t)
	base := t.TempDir()
	src := filepath.Join(base, "up")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# v1"), 0o644)
	code, out := e.runJSON(t, "add", "--local", src)
	if code != ExitOK {
		t.Fatalf("add 失败: %s", e.stderr.String())
	}
	requireString(t, out, "id", "up")

	// 修改来源内容后再 update，内容应刷新。
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# v2"), 0o644)
	code, out = e.runJSON(t, "update", "up")
	if code != ExitOK {
		t.Fatalf("update 失败: %s", e.stderr.String())
	}
	requireString(t, out, "id", "up")
	data, _ := os.ReadFile(filepath.Join(e.root, "up", "SKILL.md"))
	if string(data) != "# v2" {
		t.Errorf("update 后内容应为 v2, got %q", data)
	}
	if _, ok := out["snapshotDir"]; !ok {
		t.Error("update 应产出快照目录")
	}
	// 未知技能 update：退出码 1。
	if code := e.run("update", "ghost"); code != ExitError {
		t.Errorf("update ghost 应退出 1, got %d", code)
	}
}

func TestImportMetaDryRun(t *testing.T) {
	e := newEnv(t)
	e.writeSkill("locked")
	e.writeSkill("unlocked")
	// 根目录同级的 lock 文件。
	lock := map[string]any{
		"version": 3,
		"skills": map[string]any{
			"locked": map[string]any{
				"source": "owner/repo", "sourceType": "github",
				"sourceUrl": "https://github.com/owner/repo.git",
				"skillPath": "skills/locked", "installedAt": "2026-01-01T00:00:00Z",
				"updatedAt": "2026-01-01T00:00:00Z",
			},
		},
	}
	lockData, _ := json.Marshal(lock)
	lockPath := filepath.Join(filepath.Dir(e.root), ".skill-lock.json")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}

	// dry-run：只报告不写入。
	code, out := e.runJSON(t, "import-meta", "--dry-run")
	if code != ExitOK {
		t.Fatalf("import-meta --dry-run 失败: %s", e.stderr.String())
	}
	requireString(t, out, "lockPath", lockPath)
	if v, ok := out["dryRun"]; !ok || v != true {
		t.Errorf("dryRun 应为 true")
	}
	want, ok := out["wouldImport"].([]any)
	if !ok || len(want) != 1 || want[0] != "locked" {
		t.Errorf("wouldImport 应为 [locked], got %v", out["wouldImport"])
	}
	if _, err := os.Stat(filepath.Join(e.root, "locked", store.MetaFileName)); !os.IsNotExist(err) {
		t.Error("dry-run 绝不能写元数据文件")
	}

	// 真实导入。
	code, out = e.runJSON(t, "import-meta")
	if code != ExitOK {
		t.Fatalf("import-meta 失败: %s", e.stderr.String())
	}
	imported, ok := out["imported"].([]any)
	if !ok || len(imported) != 1 || imported[0] != "locked" {
		t.Errorf("imported 应为 [locked], got %v", out["imported"])
	}
	if _, err := os.Stat(filepath.Join(e.root, "locked", store.MetaFileName)); err != nil {
		t.Errorf("导入后元数据应存在: %v", err)
	}
}

func TestDoctorCleanAndDrift(t *testing.T) {
	// 干净根目录：退出码 0，problems 为空数组。
	e := newEnv(t)
	e.installWithMeta(t, "clean")
	code, out := e.runJSON(t, "doctor")
	if code != ExitOK {
		t.Fatalf("干净根目录 doctor 应退出 0, got %d, stderr=%s", code, e.stderr.String())
	}
	requireString(t, out, "root", e.root)
	if probs, ok := out["problems"].([]any); !ok || len(probs) != 0 {
		t.Errorf("problems 应为 [], got %v", out["problems"])
	}

	// 漂移：改动内容但不刷新元数据。
	if err := os.WriteFile(filepath.Join(e.root, "clean", "SKILL.md"), []byte("# drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out = e.runJSON(t, "doctor")
	if code != ExitDoctorProblem {
		t.Fatalf("漂移 doctor 应退出 3, got %d", code)
	}
	probs, ok := out["problems"].([]any)
	if !ok || len(probs) == 0 {
		t.Fatal("漂移时 problems 不应为空")
	}
	p0 := probs[0].(map[string]any)
	for _, key := range []string{"kind", "skill", "detail"} {
		if _, ok := p0[key]; !ok {
			t.Errorf("problem 缺少字段 %q: %v", key, p0)
		}
	}
	requireString(t, p0, "kind", KindHashDrift)
	requireString(t, p0, "skill", "clean")
}

func TestDoctorProblemKinds(t *testing.T) {
	e := newEnv(t)
	e.writeSkill("no-meta") // no-meta
	e.writeSkill("corrupt") // meta-parse
	os.WriteFile(filepath.Join(e.root, "corrupt", store.MetaFileName), []byte("{broken"), 0o644)
	// no-skillmd：只建目录，不放 SKILL.md。
	if err := os.MkdirAll(filepath.Join(e.root, "no-skillmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(e.root, "not-a-dir"), []byte("x"), 0o644) // not-a-dir

	code, out := e.runJSON(t, "doctor")
	if code != ExitDoctorProblem {
		t.Fatalf("问题根目录 doctor 应退出 3, got %d", code)
	}
	got := map[string]map[string]any{} // kind+skill -> problem
	probs := out["problems"].([]any)
	for _, raw := range probs {
		p := raw.(map[string]any)
		got[fmt.Sprintf("%s/%s", p["kind"], p["skill"])] = p
	}
	for _, key := range []string{"no-meta/no-meta", "meta-parse/corrupt", "no-skillmd/no-skillmd", "not-a-dir/not-a-dir"} {
		if _, ok := got[key]; !ok {
			t.Errorf("应报告 %s, got %v", key, probs)
		}
	}
}

func TestDoctorSafeOnHostileRoot(t *testing.T) {
	// 极端根目录：技能文件夹是文件、元数据全是乱码——doctor 只报告不 panic。
	e := newEnv(t)
	e.writeSkill("broken-meta")
	os.WriteFile(filepath.Join(e.root, "broken-meta", store.MetaFileName), []byte("\x00\xff 不是 JSON"), 0o644)
	// 名为 "file-skill" 的"技能"其实是个文件（store.Scan 会跳过它）。
	os.WriteFile(filepath.Join(e.root, "file-skill"), []byte("i am a file"), 0o644)

	code, out := e.runJSON(t, "doctor")
	if code != ExitDoctorProblem {
		t.Fatalf("doctor 应退出 3, got %d", code)
	}
	probs := out["problems"].([]any)
	if len(probs) < 2 {
		t.Errorf("至少应报告 meta-parse 与 not-a-dir, got %v", probs)
	}
	// disabled 目录里也放一个损坏元数据，确保侧目录检查同样安全。
	os.MkdirAll(filepath.Join(e.home, "disabled", "bad"), 0o755)
	os.WriteFile(filepath.Join(e.home, "disabled", "bad", store.MetaFileName), []byte("!!!"), 0o644)
	code, out = e.runJSON(t, "doctor")
	if code != ExitDoctorProblem {
		t.Fatalf("doctor 应退出 3, got %d", code)
	}
	found := false
	for _, raw := range out["problems"].([]any) {
		p := raw.(map[string]any)
		if p["kind"] == KindMetaParse && p["skill"] == "bad" {
			found = true
		}
	}
	if !found {
		t.Errorf("disabled 中的损坏元数据应被报告: %v", out["problems"])
	}
}

func TestParseArgsInterleavedFlags(t *testing.T) {
	// parseArgs 必须支持 flag 与位置参数交错（如 remove NAME --stamp TS）。
	g := &globalFlags{}
	fs := newFlagSet("test", &bytes.Buffer{}, g, nil)
	stamp := fs.String("stamp", "", "")
	pos, err := parseArgs(fs, []string{"name", "--stamp", "T1", "--json", "extra"})
	if err != nil {
		t.Fatalf("parseArgs 失败: %v", err)
	}
	if len(pos) != 2 || pos[0] != "name" || pos[1] != "extra" {
		t.Errorf("位置参数应为 [name extra], got %v", pos)
	}
	if *stamp != "T1" {
		t.Errorf("stamp 应为 T1, got %q", *stamp)
	}
	if !g.json {
		t.Error("--json 应被解析")
	}
}

func TestEnvVarRoot(t *testing.T) {
	// KURA_ROOT/KURA_HOME 与 --root/--home 等价，flag 优先。
	e := newEnv(t)
	t.Setenv("KURA_ROOT", e.root)
	t.Setenv("KURA_HOME", e.home)
	e.writeSkill("envskill")
	// 不传 --root/--home，走环境变量。
	e.stdout.Reset()
	code := Run([]string{"--json", "list"}, e.stdout, e.stderr)
	if code != ExitOK {
		t.Fatalf("环境变量模式 list 失败: %s", e.stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(e.stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	requireString(t, out, "root", e.root)
	skills := out["skills"].([]any)
	if len(skills) != 1 {
		t.Errorf("应看到 envskill, got %v", skills)
	}
}
