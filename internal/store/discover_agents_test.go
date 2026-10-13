package store

// 自动发现 agent 技能目录的测试。假主目录全在 t.TempDir() 里，绝不碰真实的
// ~/.claude、~/.kura 等。

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// newDiscoverFixture 造一个假主目录，默认根目录与数据目录都在它下面。
func newDiscoverFixture(t *testing.T) (home string, base *Config) {
	t.Helper()
	home = t.TempDir()
	base, err := NewConfig(filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	return home, base
}

// mkdirUnder 在 home 下建目录（rel 用斜杠分隔）。
func mkdirUnder(t *testing.T, home, rel string) string {
	t.Helper()
	dir := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestAgentSkillDirsOnlyExistingDirs 验证只返回真实存在的文件夹，普通文件与
// 缺失的路径都跳过；空主目录返回 nil（不去查相对路径）。
func TestAgentSkillDirsOnlyExistingDirs(t *testing.T) {
	home, _ := newDiscoverFixture(t)
	claude := mkdirUnder(t, home, ".claude/skills")
	mkdirUnder(t, home, ".codex")
	// ~/.cursor/skills 是普通文件：不是文件夹，应跳过。
	mkdirUnder(t, home, ".cursor")
	if err := os.WriteFile(filepath.Join(home, ".cursor", "skills"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pi := mkdirUnder(t, home, ".pi/agent/skills")

	got := AgentSkillDirs(home)
	if !slices.Equal(got, []string{claude, pi}) {
		t.Errorf("AgentSkillDirs = %v，想要 %v（只含存在的目录，按表中顺序）", got, []string{claude, pi})
	}
	if AgentSkillDirs("") != nil {
		t.Error("空主目录应返回 nil")
	}
}

// TestDiscoverAgentRootsAddsOnceAndPersists 验证发现结果写进 config.json 的
// roots；再次发现不重复加入、不写盘。
func TestDiscoverAgentRootsAddsOnceAndPersists(t *testing.T) {
	home, base := newDiscoverFixture(t)
	claude := mkdirUnder(t, home, ".claude/skills")
	pi := mkdirUnder(t, home, ".pi/agent/skills")

	lib, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	added, err := lib.DiscoverAgentRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(added, []string{claude, pi}) {
		t.Fatalf("新加入 = %v，想要 %v", added, []string{claude, pi})
	}
	if !slices.Equal(lib.Roots, []string{base.Root, claude, pi}) {
		t.Errorf("内存里的 Roots = %v", lib.Roots)
	}

	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(again.Roots, []string{base.Root, claude, pi}) {
		t.Errorf("重新读取后 Roots = %v，想要默认目录在第一个、然后是两个发现的目录", again.Roots)
	}
	none, err := again.DiscoverAgentRoots(home)
	if err != nil || len(none) != 0 {
		t.Errorf("再次发现应什么也不加，得到 %v err=%v", none, err)
	}
}

// TestDiscoverAgentRootsRemovedStaysRemoved 验证用户从列表移除的发现目录不会在
// 下次启动被加回来，而之后新出现的目录仍会被加入。
func TestDiscoverAgentRootsRemovedStaysRemoved(t *testing.T) {
	home, base := newDiscoverFixture(t)
	claude := mkdirUnder(t, home, ".claude/skills")
	lib, _ := LoadLibrary(base)
	if _, err := lib.DiscoverAgentRoots(home); err != nil {
		t.Fatal(err)
	}
	if err := lib.RemoveRoot(claude); err != nil {
		t.Fatal(err)
	}

	again, _ := LoadLibrary(base)
	if added, err := again.DiscoverAgentRoots(home); err != nil || len(added) != 0 {
		t.Fatalf("已移除的目录不应被加回，得到 %v err=%v", added, err)
	}
	if slices.Contains(again.Roots, claude) {
		t.Errorf("移除后 Roots = %v，不应含 %s", again.Roots, claude)
	}

	codex := mkdirUnder(t, home, ".codex/skills")
	added, err := again.DiscoverAgentRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(added, []string{codex}) {
		t.Errorf("之后新出现的 %s 应被加入，得到 %v", codex, added)
	}
}

// TestDiscoverAgentRootsSkipsSameDirectory 验证：用户已手动加入的目录只记入
// 已考虑、不重复加入；指向默认目录的符号链接也不当作新目录加入。
func TestDiscoverAgentRootsSkipsSameDirectory(t *testing.T) {
	home, base := newDiscoverFixture(t)
	if err := os.MkdirAll(base.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := mkdirUnder(t, home, ".claude/skills")
	lib, _ := LoadLibrary(base)
	if err := lib.AddRoot(claude); err != nil {
		t.Fatal(err)
	}
	// 符号链接 ~/.kiro/skills → 默认目录：同一处，不应加入。
	kiro := filepath.Join(home, ".kiro", "skills")
	if err := os.MkdirAll(filepath.Dir(kiro), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base.Root, kiro); err != nil {
		t.Skipf("无法创建符号链接（平台或权限限制）: %v", err)
	}

	added, err := lib.DiscoverAgentRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("已在列表里或指向默认目录的路径不应加入，得到 %v", added)
	}
	if len(lib.Roots) != 2 {
		t.Errorf("Roots = %v，想要 默认目录 + 手动加入的目录", lib.Roots)
	}
}

// TestDiscoverAgentRootsKeepsCorruptConfig 验证 config.json 损坏时返回错误且
// 不覆盖文件，手改的内容原样保留。
func TestDiscoverAgentRootsKeepsCorruptConfig(t *testing.T) {
	home, base := newDiscoverFixture(t)
	mkdirUnder(t, home, ".claude/skills")
	if err := os.MkdirAll(base.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := []byte("{ not json")
	if err := os.WriteFile(base.ConfigPath(), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	lib, _ := LoadLibrary(base)
	if _, err := lib.DiscoverAgentRoots(home); err == nil {
		t.Fatal("config.json 损坏时应返回错误")
	}
	got, _ := os.ReadFile(base.ConfigPath())
	if string(got) != string(broken) {
		t.Errorf("损坏的 config.json 不应被覆盖，得到 %q", got)
	}
}
