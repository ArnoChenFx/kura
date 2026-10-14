package store

// 目录列表的「读—改—写」并发安全（PR #4 评审缺陷 1）：增、删、排序基于锁内
// 刚从 config.json 读到的列表计算，而不是基于调用方内存里可能过时的副本，
// 否则别处（另一份 Library、自动发现）刚写下的目录会被覆盖掉。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// mkSubdir 在 dir 下建一个子目录并返回它的路径。
func mkSubdir(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// diskRoots 重新读 config.json 并返回合并后的目录列表。
func diskRoots(t *testing.T, base *Config) []string {
	t.Helper()
	l, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	return l.Roots
}

// TestAddRootFromStaleCopiesKeepsBoth 验证：两份各自加载的副本各加一个不同的
// 目录，两个改动都在 config.json 里，且后保存的那份内存列表与磁盘一致。
func TestAddRootFromStaleCopiesKeepsBoth(t *testing.T) {
	base, extra := newLibraryFixture(t)
	other := mkSubdir(t, filepath.Dir(extra), "other2")
	a, _ := LoadLibrary(base)
	b, _ := LoadLibrary(base)

	if err := a.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	want := []string{base.Root, extra, other}
	if got := diskRoots(t, base); !slices.Equal(got, want) {
		t.Errorf("config.json 的目录 = %v，想要两次改动都在 %v", got, want)
	}
	if !slices.Equal(b.Roots, want) {
		t.Errorf("后保存那份的内存列表 = %v，应与磁盘一致 %v", b.Roots, want)
	}
}

// TestAddRootDuplicateJudgedAgainstLatest 验证：「已在列表里」对锁内最新的
// 列表判断：另一份副本刚加过同一目录时，过时副本再加返回 ErrRootExists。
func TestAddRootDuplicateJudgedAgainstLatest(t *testing.T) {
	base, extra := newLibraryFixture(t)
	a, _ := LoadLibrary(base)
	b, _ := LoadLibrary(base)
	if err := a.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRoot(extra); !errors.Is(err, ErrRootExists) {
		t.Fatalf("过时副本重复添加应返回 ErrRootExists，得到 %v", err)
	}
	if got := diskRoots(t, base); len(got) != 2 {
		t.Errorf("目录不应重复，得到 %v", got)
	}
}

// TestRemoveRootFromStaleCopyKeepsOthersAdditions 验证：过时副本移除目录时，
// 别处刚加入的目录不会被一起抹掉。
func TestRemoveRootFromStaleCopyKeepsOthersAdditions(t *testing.T) {
	base, extra := newLibraryFixture(t)
	added := mkSubdir(t, filepath.Dir(extra), "added")
	seed, _ := LoadLibrary(base)
	if err := seed.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	a, _ := LoadLibrary(base)
	b, _ := LoadLibrary(base) // 此时只知道 [默认, extra]

	if err := a.AddRoot(added); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveRoot(extra); err != nil {
		t.Fatal(err)
	}
	want := []string{base.Root, added}
	if got := diskRoots(t, base); !slices.Equal(got, want) {
		t.Errorf("config.json 的目录 = %v，想要 %v", got, want)
	}
}

// TestMoveRootFromStaleCopyUsesLatestList 验证：排序的越界与位置对锁内最新的
// 列表判断，别处刚加入的目录保留。
func TestMoveRootFromStaleCopyUsesLatestList(t *testing.T) {
	base, extra := newLibraryFixture(t)
	added := mkSubdir(t, filepath.Dir(extra), "added")
	seed, _ := LoadLibrary(base)
	if err := seed.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	a, _ := LoadLibrary(base)
	b, _ := LoadLibrary(base) // 过时：只有 2 项，位置 2 在它看来越界
	if err := a.AddRoot(added); err != nil {
		t.Fatal(err)
	}
	if err := b.MoveRoot(extra, 2); err != nil {
		t.Fatalf("最新列表有 3 项，位置 2 不越界，得到 %v", err)
	}
	want := []string{base.Root, added, extra}
	if got := diskRoots(t, base); !slices.Equal(got, want) {
		t.Errorf("config.json 的目录 = %v，想要 %v", got, want)
	}
	// 既有语义不变：默认目录不能移，越界仍报错。
	if err := b.MoveRoot(base.Root, 1); !errors.Is(err, ErrDefaultRoot) {
		t.Errorf("默认目录不能移动，得到 %v", err)
	}
	if err := b.MoveRoot(extra, 3); err == nil {
		t.Error("位置超出最新列表范围应报错")
	}
}

// TestDiscoveredRootNotCrowdedOutByStaleSave 验证这次修复的核心动机：启动时
// 自动发现写入的目录，不会被另一份基于旧副本的目录操作挤掉。discovered
// 里已经记过它，被挤掉就永远回不来。
func TestDiscoveredRootNotCrowdedOutByStaleSave(t *testing.T) {
	home, base := newDiscoverFixture(t)
	claude := mkdirUnder(t, home, ".claude/skills")
	manual := mkdirUnder(t, home, "manual")

	startup, _ := LoadLibrary(base)
	stale, _ := LoadLibrary(base) // 在自动发现写盘之前加载的副本

	added, err := startup.DiscoverAgentRoots(home)
	if err != nil || !slices.Equal(added, []string{claude}) {
		t.Fatalf("自动发现 = %v, %v，想要 [%s]", added, err, claude)
	}
	if err := stale.AddRoot(manual); err != nil {
		t.Fatal(err)
	}

	want := []string{base.Root, claude, manual}
	if got := diskRoots(t, base); !slices.Equal(got, want) {
		t.Fatalf("config.json 的目录 = %v，自动发现的目录被挤掉了，想要 %v", got, want)
	}
	// 下次启动：发现过的路径不再重复加入，它也仍在列表里。
	next, _ := LoadLibrary(base)
	if again, err := next.DiscoverAgentRoots(home); err != nil || len(again) != 0 {
		t.Errorf("再次发现不应有新增，得到 %v, %v", again, err)
	}
	if !slices.Equal(next.Roots, want) {
		t.Errorf("重启后的列表 = %v，想要 %v", next.Roots, want)
	}
}

// TestConcurrentAddRootsAllPersist 在 -race 下跑：多份副本同时各加一个目录，
// 全部都保住。
func TestConcurrentAddRootsAllPersist(t *testing.T) {
	base, extra := newLibraryFixture(t)
	const n = 8
	var dirs []string
	for i := 0; i < n; i++ {
		dirs = append(dirs, mkSubdir(t, filepath.Dir(extra), fmt.Sprintf("d%d", i)))
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for _, d := range dirs {
		lib, _ := LoadLibrary(base) // 每份都是只含默认目录的旧副本
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- lib.AddRoot(d)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := diskRoots(t, base)
	if len(got) != n+1 {
		t.Fatalf("应保住全部 %d 个追加目录，得到 %v", n, got)
	}
	for _, d := range dirs {
		if !slices.Contains(got, d) {
			t.Errorf("丢了目录 %s", d)
		}
	}
}
