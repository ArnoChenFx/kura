package store

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// 缺失目录保持空列表语义；已有普通文件必须报错，不能被 Windows 的
// 目录枚举错误或空结果伪装成「没有技能」。三个存放位置使用同一规则。
func TestScanDirectoryPathKinds(t *testing.T) {
	for _, state := range []State{StateEnabled, StateDisabled, StateTrashed} {
		t.Run(string(state), func(t *testing.T) {
			c := newTestConfig(t)
			path, scan := c.Root, c.Scan
			switch state {
			case StateDisabled:
				path, scan = c.DisabledRoot(), c.ScanDisabled
			case StateTrashed:
				path, scan = c.TrashRoot(), c.ScanTrash
			}
			if got, err := scan(); err != nil || len(got) != 0 {
				t.Fatalf("missing directory: got %v, err %v", got, err)
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if got, err := scan(); err != nil || len(got) != 0 {
				t.Fatalf("empty directory: got %v, err %v", got, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			content := []byte("existing file, not a skill root")
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := scan()
			if got != nil || !errors.Is(err, ErrNotDir) || errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("plain file must report ErrNotDir: got %v, err %v", got, err)
			}
			if state == StateEnabled {
				if got, err := c.ScanAll(); got != nil || !errors.Is(err, ErrNotDir) {
					t.Fatalf("ScanAll must retain root error: got %v, err %v", got, err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(content) {
				t.Fatalf("scan altered the file: %q, err %v", after, err)
			}
		})
	}
}
