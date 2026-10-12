package main

import (
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	myui "github.com/egoist/mygo/ui"
)

// TestMainBuildsWindow 验证 main.go 的窗口配置与视图装配是连通的：
// 用临时目录的 store 配置驱动 internal/ui 的 View（不打开真实窗口）。
func TestMainBuildsWindow(t *testing.T) {
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
}
