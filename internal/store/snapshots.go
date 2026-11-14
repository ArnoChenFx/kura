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

// validateSnapshotFiles 检查「历史副本」：它必须是 kura 自己写下的真实目录树，任何链接、设备、FIFO 都不接受。
// 这里刻意仍用 filepath.WalkDir：不跟随任何链接，链接是否存在本身就是要抓的问题。
// 当前副本（用户自己的技能目录）走 validateCurrentFiles。
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

// validateCurrentFiles 检查「当前副本」：链接形式的技能，或技能里含有链接目录（Windows 联接点、目录符号
// 链接）都允许——它们会被穿透复制（环被跳过）。其他仍与以前一样拒绝：指向文件的符号链接、
// 断链、设备、FIFO，它们没法当作普通文件完整备份。walkTree 把链接目录报成目录项，所以这里只需要
// 拦住不是目录也不是普通文件的项。
func validateCurrentFiles(dir string) error {
	return walkTree(dir, walkOpts{}, func(e treeEntry) error {
		if e.IsDir || e.Mode.IsRegular() {
			return nil
		}
		return invalidSnapshot(e.Path)
	})
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
	// 从配置根到技能目录的各级必须是真实目录。唯独技能目录自己可以是链接（联接点、目录符号链接）：
	// 它必须能解析到一个存在的目录；恢复时作用在它指向的真实目录上，链接本身不动（见 restoreSnapshot）。
	if err := snapshotPathSafe(base, filepath.Dir(dir), false); err != nil {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", invalidSnapshot(dir)
	}
	if isLinkMode(fi.Mode()) {
		if _, ok := realDir(dir); !ok {
			return "", invalidSnapshot(dir)
		}
	} else if !fi.IsDir() {
		return "", invalidSnapshot(dir)
	}
	// 当前技能可能正因正文/元数据损坏才需要恢复；只拒绝指向文件的链接、断链与特殊文件，
	// 完整原样备份这些内容，历史来源自身则必须通过更严格的完整性检查。
	if err := validateCurrentFiles(dir); err != nil {
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
	// walkTree 会进入当前副本里的链接目录（链接形式的技能本身也是）；历史副本已经确认不含链接，行为与 WalkDir 一致。
	for side, dir := range []string{current, source} {
		if err := walkTree(dir, walkOpts{}, func(e treeEntry) error {
			if !e.IsDir {
				paths[e.Rel] = true
				files[side][e.Rel] = true
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
	// 用 walkTree：当前副本可以是链接形式的技能，要进入它的目录，不然指纹永远是空目录的指纹。
	if err := walkTree(dir, walkOpts{}, func(e treeEntry) error {
		if !e.IsDir {
			paths = append(paths, e.Rel)
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
	// 当前副本是链接（联接点、目录符号链接）时，替换作用在它指向的真实目录上，链接本身不动；
	// 暗存目录与备份容器也建在真实目录旁边（与它同卷，最后的 rename 才是原子的）。
	// 链接可以指到任何地方，所以先过 checkReplaceTarget（盘符根、包含 kura 自己目录的目标一律拒绝）。
	target := current
	if fi, lerr := os.Lstat(current); lerr == nil && isLinkMode(fi.Mode()) {
		real, ok := realDir(current)
		if !ok {
			return nil, invalidSnapshot(current)
		}
		if err := c.checkReplaceTarget(b.name, real); err != nil {
			return nil, err
		}
		target = real
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".kura-restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := copySnapshotTree(source, stage, true); err != nil {
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
	if err := copySnapshotTree(current, saved, false); err != nil {
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
	holding, err := os.MkdirTemp(filepath.Dir(target), ".kura-previous-")
	if err != nil {
		return nil, err
	}
	old := filepath.Join(holding, "content")
	if err := rename(target, old); err != nil {
		os.Remove(holding)
		return nil, err
	}
	if err := rename(stage, target); err != nil {
		if rollbackErr := rename(old, target); rollbackErr != nil {
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
//
// strict 为真用于历史副本（必须是 kura 自己写下的真实目录树）：根或树里出现任何链接都拒绝，绝不
// 跟随。strict 为假用于当前副本（用户自己的技能目录）：链接形式的技能、树里的链接目录都穿透复制成
// 真实内容（环被跳过），其他非普通文件仍拒绝。
func copySnapshotTree(source, dest string, strict bool) error {
	type dirMode struct {
		path string
		mode os.FileMode
	}
	var dirs []dirMode
	if strict {
		// walkTree 会解析根自己的链接，严格模式不允许，所以先查。
		if fi, err := os.Lstat(source); err != nil {
			return err
		} else if isLinkMode(fi.Mode()) {
			return invalidSnapshot(source)
		}
	}
	err := walkTree(source, walkOpts{noSubLinks: strict}, func(entry treeEntry) error {
		path := entry.Path
		if strict && isLinkMode(entry.Mode) {
			return invalidSnapshot(path)
		}
		target := filepath.Join(dest, filepath.FromSlash(entry.Rel))
		if entry.IsDir {
			// os.Stat 跟随链接：链接目录取目标目录的权限。
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			dirs = append(dirs, dirMode{target, info.Mode().Perm()})
			// 复制完子文件后再收紧目录权限，原本只读的目录也能完整准备。
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Mode.IsRegular() {
			return invalidSnapshot(path)
		}
		info, err := os.Lstat(path)
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
