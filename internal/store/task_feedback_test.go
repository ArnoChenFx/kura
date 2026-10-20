package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 已取消的批量检查保留每项身份，不读来源、不写快照，也不会启动 git。
func TestPreviewUpdatesContextAlreadyCancelled(t *testing.T) {
	c := newTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	names := []string{"one", "two"}
	results := c.PreviewUpdatesContext(ctx, names)
	for i, result := range results {
		if result.Name != names[i] || result.Preview != nil || !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("result[%d] = %+v", i, result)
		}
	}
	if entries, err := os.ReadDir(c.Home); err == nil && len(entries) != 0 {
		t.Fatalf("cancelled check wrote state: %v", entries)
	}
}

// 假 git 只等待本地进程；取消必须结束当前命令，而不是等整体超时。
func TestGitOutputContextCancelsRunningProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake git; context-before-start is covered on all platforms")
	}
	isolatedGitEnv(t)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep unavailable")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	script := "#!/bin/sh\n: > " + quote(marker) + "\nexec " + quote(sleep) + " 30\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := gitOutputContext(ctx, time.Minute, "", "", "ls-remote", "unused")
		done <- err
	}()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("fake git exited before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("fake git did not start")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled git kept running")
	}
}

// clone 后取消仍清理临时目录，已安装技能与元数据保持原样。
func TestPreviewCancellationCleansClone(t *testing.T) {
	isolatedGitEnv(t)
	c := newTestConfig(t)
	repo, mirror := twoSkillRepo(t, c, "cancel")
	advanceUpstream(t, repo, "a", map[string]string{"SKILL.md": "updated\n"})
	mirror.sync(repo)
	before, err := HashDir(c.SkillPath("a"))
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloneHook = func(string) { cancel() }
	t.Cleanup(func() { cloneHook = nil })
	_, err = c.PreviewUpdateContext(ctx, "a")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	after, err := HashDir(c.SkillPath("a"))
	if err != nil || before != after {
		t.Fatalf("cancelled preview changed installed content: %s -> %s (%v)", before, after, err)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary clone left behind: %v (%v)", entries, err)
	}
}

// 进度不是计时器模拟：本地安装跳过下载，进入写入阶段时目标尚未创建。
func TestAddWithProgressLocalPhases(t *testing.T) {
	c := newTestConfig(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: progress\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var phases []InstallPhase
	res, err := c.AddWithProgress(&AddOptions{LocalPath: src}, func(phase InstallPhase) {
		phases = append(phases, phase)
		if fileExists(c.SkillPath("progress")) {
			t.Fatal("phase callback must precede installation write")
		}
	})
	if err != nil || res.ID != "progress" {
		t.Fatalf("AddWithProgress = %+v, %v", res, err)
	}
	if want := []InstallPhase{InstallInspect, InstallWrite}; !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
}

func TestAddManyWithProgressSharesDownload(t *testing.T) {
	multiSkillRepo(t, "owner/progress", "skills/a", "skills/b")
	c := newTestConfig(t)
	clones := countClones(t)
	var phases []InstallPhase
	results := c.AddManyWithProgress("owner/progress", "", []AddSpec{{SubPath: "skills/a"}, {SubPath: "skills/b"}}, func(phase InstallPhase) {
		phases = append(phases, phase)
	})
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	if want := []InstallPhase{InstallDownload, InstallInspect, InstallWrite, InstallInspect, InstallWrite}; !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	if *clones != 1 {
		t.Fatalf("clones = %d, want 1", *clones)
	}
}

func TestAddSkillByNameWithProgress(t *testing.T) {
	multiSkillRepo(t, "owner/market-progress", "skills/a", "skills/b")
	c := newTestConfig(t)
	var phases []InstallPhase
	result, err := c.AddSkillByNameWithProgress("owner/market-progress", "a", func(phase InstallPhase) {
		phases = append(phases, phase)
	})
	if err != nil || result.ID != "a" {
		t.Fatalf("AddSkillByNameWithProgress = %+v, %v", result, err)
	}
	if want := []InstallPhase{InstallDownload, InstallInspect, InstallDownload, InstallInspect, InstallWrite}; !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
}
