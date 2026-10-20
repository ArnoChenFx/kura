package store

// 历史恢复只接受本次预览产生的计划：目录、状态和两边完整内容都绑定在计划中。
// 先复制并核验恢复内容、保存当前快照，最后用同盘 rename 交换；失败不先删当前目录。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

var (
	ErrSnapshotInvalid = i18n.NewError("storeErrSnapshotInvalid", func() string { return locales.StoreErrSnapshotInvalid("") })
	ErrSnapshotChanged = i18n.NewError("storeErrSnapshotChanged", func() string { return locales.StoreErrSnapshotChanged() })
	ErrSnapshotState   = i18n.NewError("storeErrSnapshotState", func() string { return locales.StoreErrSnapshotState() })
)

// Snapshot 是当前根目录下某技能的一份历史副本；有问题的项仍列出，供界面说明。
type Snapshot struct {
	Stamp string
	Time  time.Time
	Err   error
}

// SnapshotPreview 的公开部分用于显示，私有字段绑定恢复计划，调用方不能拼出任意路径。
type SnapshotPreview struct {
	Name            string
	State           State
	Stamp           string
	Files           []FileDiff
	MetadataChanged bool
	binding         snapshotBinding
}

type snapshotBinding struct {
	root, home, scope, name, stamp string
	state                          State
	currentHash, snapshotHash      string
}

func (p *SnapshotPreview) HasChanges() bool {
	return p != nil && p.binding.currentHash != p.binding.snapshotHash
}

func invalidSnapshot(path string) error {
	return sentinelErr(ErrSnapshotInvalid, "storeErrSnapshotInvalid", func() string { return locales.StoreErrSnapshotInvalid(path) })
}

// snapshotPathSafe 拒绝从配置根开始的任一链接/联接点；不存在的尾段只在创建前允许。
func snapshotPathSafe(base, path string, allowMissing bool) error {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return invalidSnapshot(path)
	}
	p := filepath.Clean(base)
	parts := []string{"."}
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	for _, part := range parts {
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if err != nil {
			if allowMissing && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return invalidSnapshot(p)
		}
		if !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return invalidSnapshot(p)
		}
	}
	return nil
}

// validateSnapshotTree 不跟随链接，不接受设备/FIFO，也不让损坏元数据被恢复进技能目录。
// 元数据缺失是合法的本地技能；SKILL.md 必须是普通文件。
func validateSnapshotTree(dir, name string) error {
	if err := validateSnapshotFiles(dir); err != nil {
		return err
	}
	fi, err := os.Lstat(filepath.Join(dir, "SKILL.md"))
	if err != nil || !fi.Mode().IsRegular() {
		return invalidSnapshot(dir)
	}
	m := readMeta(dir)
	if m.Err != nil || (m.Found && (m.Meta.MetaVersion != MetaVersion || (m.Meta.ID != "" && m.Meta.ID != name))) {
		return invalidSnapshot(dir)
	}
	return nil
}

func validateSnapshotFiles(dir string) error {
	if err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 || (!e.IsDir() && !e.Type().IsRegular()) {
			return invalidSnapshot(p)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (c *Config) snapshotTarget(name string, state State) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	var base, dir string
	switch state {
	case StateEnabled:
		base, dir = c.Root, c.SkillPath(name)
	case StateDisabled:
		base, dir = c.Home, c.DisabledPath(name)
	default:
		return "", ErrSnapshotState
	}
	if err := snapshotPathSafe(base, dir, false); err != nil {
		return "", err
	}
	// 当前技能可能正因正文/元数据损坏才需要恢复；只拒绝链接与特殊文件，
	// 完整原样备份这些内容，历史来源自身则必须通过更严格的完整性检查。
	if err := validateSnapshotFiles(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Config) snapshotSource(name, stampName string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	if !ValidStampName(stampName) {
		return "", invalidSnapshot(stampName)
	}
	dir := c.SnapshotPath(name, stampName)
	if err := snapshotPathSafe(c.Home, dir, false); err != nil {
		return "", err
	}
	if err := validateSnapshotTree(dir, name); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Config) ListSnapshots(name string) ([]Snapshot, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	dir := filepath.Join(c.SnapshotsRoot(), name)
	if err := snapshotPathSafe(c.Home, dir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrReadDir", func() string { return locales.StoreErrReadDir(dir) })
	}
	var out []Snapshot
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s := Snapshot{Stamp: e.Name()}
		if len(s.Stamp) >= 15 {
			s.Time, _ = time.ParseInLocation("20060102-150405", s.Stamp[:15], time.Local)
		}
		_, s.Err = c.snapshotSource(name, s.Stamp)
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Snapshot) int { return strings.Compare(b.Stamp, a.Stamp) })
	return out, nil
}

// PreviewSnapshot 比较当前副本与历史内容；指纹包含元数据，防止预览后来源被换掉。
func (c *Config) PreviewSnapshot(name string, state State, stampName string) (*SnapshotPreview, error) {
	current, err := c.snapshotTarget(name, state)
	if err != nil {
		return nil, err
	}
	source, err := c.snapshotSource(name, stampName)
	if err != nil {
		return nil, err
	}
	currentHash, err := snapshotFingerprint(current)
	if err != nil {
		return nil, err
	}
	snapshotHash, err := snapshotFingerprint(source)
	if err != nil {
		return nil, err
	}
	files, err := snapshotFileChanges(current, source)
	if err != nil {
		return nil, err
	}
	p := &SnapshotPreview{Name: name, State: state, Stamp: stampName, Files: files,
		binding: snapshotBinding{root: c.Root, home: c.Home, scope: c.Scope, name: name, stamp: stampName, state: state, currentHash: currentHash, snapshotHash: snapshotHash}}
	for _, f := range p.Files {
		if f.Path == MetaFileName {
			p.MetadataChanged = true
		}
	}
	return p, nil
}

// 历史恢复与更新不同：元数据及手工放入的 .git 也会原样恢复，因此都必须列入差异。
func snapshotFileChanges(current, source string) ([]FileDiff, error) {
	paths := map[string]bool{}
	files := [2]map[string]bool{{}, {}}
	for side, dir := range []string{current, source} {
		if err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() {
				rel, err := filepath.Rel(dir, p)
				if err != nil {
					return err
				}
				paths[filepath.ToSlash(rel)] = true
				files[side][filepath.ToSlash(rel)] = true
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	slices.Sort(names)
	var out []FileDiff
	for _, path := range names {
		var old, next snapshotFileSummary
		var oldErr, newErr error
		if files[0][path] {
			old, oldErr = summarizeSnapshotFile(filepath.Join(current, filepath.FromSlash(path)))
		}
		if files[1][path] {
			next, newErr = summarizeSnapshotFile(filepath.Join(source, filepath.FromSlash(path)))
		}
		if oldErr != nil {
			return nil, oldErr
		}
		if newErr != nil {
			return nil, newErr
		}
		if files[0][path] && files[1][path] && old.hash == next.hash {
			continue
		}
		f := FileDiff{Path: path, Status: DiffChanged, OldHash: old.hash, NewHash: next.hash}
		switch {
		case !files[0][path]:
			f.Status, f.Added = DiffAdded, len(splitLines(next.sample))
		case !files[1][path]:
			f.Status, f.Removed = DiffRemoved, len(splitLines(old.sample))
		default:
			if !old.truncated && !next.truncated {
				f.Added, f.Removed = lineDiffCounts(splitLines(old.sample), splitLines(next.sample))
			}
		}
		if old.truncated || next.truncated {
			// -1 表示未计算大文件的行统计，界面省略该项，不把截断样本冒充完整差异。
			f.Added, f.Removed = -1, -1
		}
		out = append(out, f)
	}
	return out, nil
}

type snapshotFileSummary struct {
	hash      string
	sample    []byte
	truncated bool
}

// snapshotSample 只保留预览所需的前 64 KiB，完整文件仍流式参与哈希。
type snapshotSample struct {
	data      []byte
	truncated bool
}

func (s *snapshotSample) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, (64<<10)-len(s.data))
	s.data = append(s.data, p[:keep]...)
	s.truncated = s.truncated || keep < n
	return n, nil
}

func summarizeSnapshotFile(path string) (snapshotFileSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return snapshotFileSummary{}, err
	}
	defer f.Close()
	h := sha256.New()
	var sample snapshotSample
	if _, err := io.Copy(io.MultiWriter(h, &sample), f); err != nil {
		return snapshotFileSummary{}, err
	}
	return snapshotFileSummary{hex.EncodeToString(h.Sum(nil)), sample.data, sample.truncated}, nil
}

// snapshotFingerprint 保持既有路径+内容哈希口径，但包含元数据并流式读取大文件。
func snapshotFingerprint(dir string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		return "", err
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, path := range paths {
		io.WriteString(h, path)
		h.Write([]byte{0})
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Config) verifySnapshotPreview(p *SnapshotPreview) (current, source string, err error) {
	if p == nil || p.binding.currentHash == "" || c.Root != p.binding.root || c.Home != p.binding.home || c.Scope != p.binding.scope {
		return "", "", ErrSnapshotChanged
	}
	b := p.binding
	current, err = c.snapshotTarget(b.name, b.state)
	if err != nil {
		return "", "", err
	}
	source, err = c.snapshotSource(b.name, b.stamp)
	if err != nil {
		return "", "", err
	}
	for _, item := range []struct{ dir, hash string }{{current, b.currentHash}, {source, b.snapshotHash}} {
		got, e := snapshotFingerprint(item.dir)
		if e != nil {
			return "", "", e
		}
		if got != item.hash {
			return "", "", ErrSnapshotChanged
		}
	}
	return current, source, nil
}

// RestoreSnapshot 完整恢复预览中的副本，保留当时的元数据和当前启用/停用状态。
func (c *Config) RestoreSnapshot(p *SnapshotPreview) (*Result, error) {
	return c.restoreSnapshot(p, os.Rename)
}

// rename 参数只用于故障注入测试：模拟第二次 rename 失败，验证回滚保留当前内容。
func (c *Config) restoreSnapshot(p *SnapshotPreview, rename func(string, string) error) (*Result, error) {
	current, source, err := c.verifySnapshotPreview(p)
	if err != nil {
		return nil, err
	}
	b := p.binding
	stage, err := os.MkdirTemp(filepath.Dir(current), ".kura-restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := copySnapshotTree(source, stage); err != nil {
		return nil, err
	}
	if got, err := snapshotFingerprint(stage); err != nil || got != b.snapshotHash {
		return nil, ErrSnapshotChanged
	}
	snapshotParent := filepath.Join(c.SnapshotsRoot(), b.name)
	if err := snapshotPathSafe(c.Home, snapshotParent, true); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(snapshotParent, 0o755); err != nil {
		return nil, err
	}
	saved, err := os.MkdirTemp(snapshotParent, stamp(time.Now())+"-")
	if err != nil {
		return nil, err
	}
	if err := copySnapshotTree(current, saved); err != nil {
		os.RemoveAll(saved)
		return nil, err
	}
	if got, err := snapshotFingerprint(saved); err != nil || got != b.currentHash {
		os.RemoveAll(saved)
		return nil, ErrSnapshotChanged
	}
	if _, _, err := c.verifySnapshotPreview(p); err != nil {
		return nil, err
	}
	// 备份与目标在同一文件系统；空容器保留唯一名字，不借用可撞名的 .kura-new。
	holding, err := os.MkdirTemp(filepath.Dir(current), ".kura-previous-")
	if err != nil {
		return nil, err
	}
	old := filepath.Join(holding, "content")
	if err := rename(current, old); err != nil {
		os.Remove(holding)
		return nil, err
	}
	if err := rename(stage, current); err != nil {
		if rollbackErr := rename(old, current); rollbackErr != nil {
			// 极端情况下保留完整旧目录与快照，绝不为清理临时目录而删掉它。
			return nil, i18n.Wrapf(err, "storeErrSnapshotReplace", func() string { return locales.StoreErrSnapshotReplace(old) })
		}
		os.Remove(holding)
		return nil, i18n.Wrapf(err, "storeErrReplaceRestored", func() string { return locales.StoreErrReplaceRestored() })
	}
	os.RemoveAll(holding)
	return &Result{ID: b.name, State: b.state, Dir: current, SnapshotDir: saved}, nil
}

// 快照复制保留文件及目录权限；每次只打开一个文件，避免大技能树耗尽文件描述符。
func copySnapshotTree(source, dest string) error {
	type dirMode struct {
		path string
		mode os.FileMode
	}
	var dirs []dirMode
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return invalidSnapshot(path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			dirs = append(dirs, dirMode{target, info.Mode().Perm()})
			// 复制完子文件后再收紧目录权限，原本只读的目录也能完整准备。
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return invalidSnapshot(path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].path, dirs[i].mode); err != nil {
			return err
		}
	}
	return nil
}

// SnapshotTextDiff 仅用于界面阅读，最多展示 400 行、每份文件读取 64 KiB。
type SnapshotTextDiff struct {
	Lines     []SnapshotDiffLine
	Truncated bool
	Binary    bool
}
type SnapshotDiffLine struct{ Kind, Text string }

func (c *Config) SnapshotFileDiff(p *SnapshotPreview, path string) (*SnapshotTextDiff, error) {
	current, source, err := c.verifySnapshotPreview(p)
	if err != nil {
		return nil, err
	}
	found, status := false, ""
	for _, f := range p.Files {
		if f.Path == path {
			found, status = true, f.Status
			break
		}
	}
	if !found || !filepath.IsLocal(filepath.FromSlash(path)) {
		return nil, invalidSnapshot(path)
	}
	read := func(dir string, exists bool) ([]byte, bool, error) {
		if !exists {
			return nil, false, nil
		}
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if len(data) > 64<<10 {
			cut := 64 << 10
			for cut > 0 && !utf8.RuneStart(data[cut]) {
				cut--
			}
			return data[:cut], true, err
		}
		return data, false, err
	}
	old, oldCut, err := read(current, status != DiffAdded)
	if err != nil {
		return nil, err
	}
	next, newCut, err := read(source, status != DiffRemoved)
	if err != nil {
		return nil, err
	}
	out := &SnapshotTextDiff{Truncated: oldCut || newCut}
	if bytes.IndexByte(old, 0) >= 0 || bytes.IndexByte(next, 0) >= 0 || !utf8.Valid(old) || !utf8.Valid(next) {
		out.Binary = true
		return out, nil
	}
	a, z := splitLines(old), splitLines(next)
	if len(a) > 400 {
		a = a[:400]
		out.Truncated = true
	}
	if len(z) > 400 {
		z = z[:400]
		out.Truncated = true
	}
	// 有界 LCS 给出真正逐行差异，而不是把整个文件删掉后重加。
	dp := make([][]uint16, len(a)+1)
	for i := range dp {
		dp[i] = make([]uint16, len(z)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(z) - 1; j >= 0; j-- {
			if a[i] == z[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	for i, j := 0, 0; i < len(a) || j < len(z); {
		if len(out.Lines) >= 400 {
			out.Truncated = true
			break
		}
		switch {
		case i < len(a) && j < len(z) && a[i] == z[j]:
			out.Lines = append(out.Lines, SnapshotDiffLine{" ", a[i]})
			i++
			j++
		case i < len(a) && (j == len(z) || dp[i+1][j] >= dp[i][j+1]):
			out.Lines = append(out.Lines, SnapshotDiffLine{"-", a[i]})
			i++
		default:
			out.Lines = append(out.Lines, SnapshotDiffLine{"+", z[j]})
			j++
		}
	}
	return out, nil
}
