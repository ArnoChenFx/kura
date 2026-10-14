package store

// 目录列表的手动排序（MoveRoot）：默认目录固定在第一个，其余目录的顺序持久化
// 在 config.json 的 roots 里。

import (
	"errors"
	"slices"
	"testing"
)

func TestMoveRootReordersAndPersists(t *testing.T) {
	base, extra := newLibraryFixture(t)
	second := t.TempDir()
	third := t.TempDir()
	lib, _ := LoadLibrary(base)
	for _, r := range []string{extra, second, third} {
		if err := lib.AddRoot(r); err != nil {
			t.Fatal(err)
		}
	}

	// [默认, extra, second, third] → 把 third 挪到 extra 的位置。
	if err := lib.MoveRoot(third, 1); err != nil {
		t.Fatal(err)
	}
	want := []string{base.Root, third, extra, second}
	if !slices.Equal(lib.Roots, want) {
		t.Fatalf("MoveRoot 之后 Roots = %v，想要 %v", lib.Roots, want)
	}
	again, _ := LoadLibrary(base)
	if !slices.Equal(again.Roots, want) {
		t.Errorf("顺序应写进 config.json，重新读取得到 %v", again.Roots)
	}

	// 挪到末尾。
	if err := lib.MoveRoot(third, 3); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lib.Roots, []string{base.Root, extra, second, third}) {
		t.Errorf("挪到末尾后 Roots = %v", lib.Roots)
	}
}

func TestMoveRootKeepsDefaultFirst(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := lib.MoveRoot(base.Root, 1); !errors.Is(err, ErrDefaultRoot) {
		t.Errorf("默认目录不能移动，得到 %v", err)
	}
	if err := lib.MoveRoot(extra, 0); !errors.Is(err, ErrDefaultRoot) {
		t.Errorf("其他目录不能挪到第一个位置，得到 %v", err)
	}
	if err := lib.MoveRoot(t.TempDir(), 1); !errors.Is(err, ErrUnknownRoot) {
		t.Errorf("不在列表里的目录应返回 ErrUnknownRoot，得到 %v", err)
	}
	if err := lib.MoveRoot(extra, 5); err == nil {
		t.Error("越界的位置应返回错误")
	}
	if !slices.Equal(lib.Roots, []string{base.Root, extra}) {
		t.Errorf("失败的移动不应改动列表：%v", lib.Roots)
	}
}
