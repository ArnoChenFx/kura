package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// Result 是所有变更操作的返回值：技能 id + 操作后的新状态。
type Result struct {
	ID          string
	State       State
	Dir         string
	SnapshotDir string
}

// stamp 生成快照/回收站目录名的时间戳（本地时间，文件名安全）。
func stamp(t time.Time) string {
	return t.Format("20060102-150405")
}

// snapshotSkill 把技能文件夹复制到 snapshots/<name>/<stamp>。复制而非移动：
// 即使后续操作失败，原位置与快照都完好。
func (c *Config) snapshotSkill(name, dir string, now time.Time) (string, error) {
	if err := c.ensureDirs(); err != nil {
		return "", err
	}
	dst := c.SnapshotPath(name, stamp(now))
	if err := copyDir(dir, dst); err != nil {
		return "", i18n.Wrapf(err, "storeErrSnapshot", func() string { return locales.StoreErrSnapshot(name) })
	}
	return dst, nil
}

// Enable 把 disabled/<name> 移回根目录。根目录已有同名技能时返回冲突错误。
func (c *Config) Enable(name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	src := c.DisabledPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSkill, name)
	}
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, i18n.NewError("storeErrSkillExists", func() string { return locales.StoreErrSkillExists(name) })
	}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		return nil, i18n.Wrapf(err, "storeErrCreateRootDir", func() string { return locales.StoreErrCreateRootDir() })
	}
	if err := moveDir(src, dst); err != nil {
		return nil, i18n.Wrapf(err, "storeErrEnableFailed", func() string { return locales.StoreErrEnableFailed(name) })
	}
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}

// Disable 把根目录的 <name> 移入 disabled/（纯移动，不复制不改内容）。
func (c *Config) Disable(name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSkill, name)
	}
	dst := c.DisabledPath(name)
	if fileExists(dst) {
		return nil, i18n.NewError("storeErrDisabledExists", func() string { return locales.StoreErrDisabledExists(name) })
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	if err := moveDir(src, dst); err != nil {
		return nil, i18n.Wrapf(err, "storeErrDisableFailed", func() string { return locales.StoreErrDisableFailed(name) })
	}
	return &Result{ID: name, State: StateDisabled, Dir: dst}, nil
}

// Remove 把根目录的技能移入 trash/<name>/<stamp>，移动前先做内容快照。
// 快照与回收站是两个动作：快照永远留在 snapshots/ 里，trash 里的副本用于恢复。
func (c *Config) Remove(name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSkill, name)
	}
	now := time.Now()
	snapDir, err := c.snapshotSkill(name, src, now)
	if err != nil {
		return nil, err
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	dst := filepath.Join(c.TrashRoot(), name, stamp(now))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, i18n.Wrapf(err, "storeErrCreateTrashDir", func() string { return locales.StoreErrCreateTrashDir() })
	}
	if err := moveDir(src, dst); err != nil {
		return nil, i18n.Wrapf(err, "storeErrMoveToTrash", func() string { return locales.StoreErrMoveToTrash(name) })
	}
	return &Result{ID: name, State: StateTrashed, Dir: dst, SnapshotDir: snapDir}, nil
}

// Restore 把 trash/<name>/<stamp> 移回根目录。stamp 为空时取最新一次。
// 根目录已有同名技能时返回冲突错误。
func (c *Config) Restore(name, stampName string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if stampName != "" && !ValidStampName(stampName) {
		return nil, sentinelErr(ErrInvalidName, "storeErrInvalidStampName", func() string { return locales.StoreErrInvalidStampName(stampName) })
	}
	trashDir := filepath.Join(c.TrashRoot(), name)
	if !fileExists(trashDir) {
		return nil, sentinelErr(ErrUnknownSkill, "storeErrUnknownSkillInTrash", func() string { return locales.StoreErrUnknownSkillInTrash(name) })
	}
	if stampName == "" {
		latest, err := latestSubdir(trashDir)
		if err != nil {
			return nil, err
		}
		stampName = latest
	}
	src := filepath.Join(trashDir, stampName)
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, i18n.NewError("storeErrSkillExists", func() string { return locales.StoreErrSkillExists(name) })
	}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		return nil, i18n.Wrapf(err, "storeErrCreateRootDir", func() string { return locales.StoreErrCreateRootDir() })
	}
	if err := moveDir(src, dst); err != nil {
		return nil, i18n.Wrapf(err, "storeErrRestoreFailed", func() string { return locales.StoreErrRestoreFailed(name) })
	}
	// 清理 trash/<name> 空壳目录：os.Remove 只能删空目录，若同一技能还有
	// 其他时间戳副本则会失败并忽略（保留它们是正确的）。
	os.Remove(trashDir)
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}

// Purge 把回收站里的技能 name 永久删除（它的全部时间戳副本）。删除前每份副本
// 都必须有一份**有效快照**在 snapshots/<name>/<stamp>：移入回收站时写下的快照
// 只有确实是目录、非符号链接、且内容哈希与回收站副本一致才沿用，否则重新写一份
// （见 ensureSnapshot）；快照写不下去就中断，保留回收站副本并清掉写到一半的
// 快照。快照本身不删，符合「破坏性操作前先快照」的约定。name 必须是单一目录名
// （ErrInvalidName），回收站里没有该技能时返回 ErrUnknownSkill。
func (c *Config) Purge(name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	trashDir := filepath.Join(c.TrashRoot(), name)
	if !fileExists(trashDir) {
		return nil, sentinelErr(ErrUnknownSkill, "storeErrUnknownSkillInTrash", func() string { return locales.StoreErrUnknownSkillInTrash(name) })
	}
	// 防御：即使名字校验漏过什么，RemoveAll 的目标也必须仍在回收站之内
	// （解析符号链接后比较），否则什么都不删。
	if !within(c.TrashRoot(), trashDir) {
		return nil, sentinelErr(ErrInvalidName, "storeErrEscapesTrash", func() string { return locales.StoreErrEscapesTrash(name) })
	}
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrTrashRead", func() string { return locales.StoreErrTrashRead(name) })
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	var snapDir string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dst, err := c.ensureSnapshot(name, e.Name(), filepath.Join(trashDir, e.Name()))
		if err != nil {
			return nil, err
		}
		snapDir = dst
	}
	if err := os.RemoveAll(trashDir); err != nil {
		return nil, i18n.Wrapf(err, "storeErrPurgeFailed", func() string { return locales.StoreErrPurgeFailed(name) })
	}
	return &Result{ID: name, SnapshotDir: snapDir}, nil
}

// ensureSnapshot 保证回收站副本 src（trash/<name>/<stampName>）在 snapshots 里
// 有一份有效快照，返回快照目录。「有效」不是路径存在而已：
//   - snapshots/<name>/<stampName> 是符号链接（含 Windows 联接点）：报错，不写
//     穿链接，也不删回收站副本；
//   - 是目录且内容哈希（HashDir）与 src 一致：沿用，不覆盖；
//   - 不存在，或是空目录（被手动清空过）：复制写入原位置；
//   - 是内容不一致的非空目录：快照只增不删，不动它，改写到同级的
//     <stampName>-N 新目录；
//   - 被普通文件占用：报错。
//
// 写完后再核对一次哈希，失败时清掉写到一半的快照并返回错误。
func (c *Config) ensureSnapshot(name, stampName, src string) (string, error) {
	want, err := HashDir(src)
	if err != nil {
		return "", i18n.Wrapf(err, "storeErrHashTrashCopy", func() string { return locales.StoreErrHashTrashCopy(name) })
	}
	dst := c.SnapshotPath(name, stampName)
	info, err := os.Lstat(dst)
	switch {
	case err == nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0:
		return "", i18n.NewError("storeErrSnapshotIsSymlink", func() string { return locales.StoreErrSnapshotIsSymlink(dst, name) })
	case err == nil && !info.IsDir():
		return "", i18n.NewError("storeErrSnapshotIsFile", func() string { return locales.StoreErrSnapshotIsFile(dst, name) })
	case err == nil:
		if got, herr := HashDir(dst); herr == nil && got == want {
			return dst, nil
		}
		if entries, rerr := os.ReadDir(dst); rerr != nil || len(entries) > 0 {
			// 非空却对不上：留着它，把这一份写到新目录。
			if dst, err = c.freshSnapshotPath(name, stampName); err != nil {
				return "", err
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", i18n.Wrapf(err, "storeErrSnapshotCheck", func() string { return locales.StoreErrSnapshotCheck(dst, name) })
	}
	if err := copyDir(src, dst); err != nil {
		// dst 之前不存在或是空目录，这里只清掉写到一半的部分副本。
		os.RemoveAll(dst)
		return "", i18n.Wrapf(err, "storeErrSnapshotBeforePurge", func() string { return locales.StoreErrSnapshotBeforePurge(name) })
	}
	if got, err := HashDir(dst); err != nil || got != want {
		os.RemoveAll(dst)
		return "", i18n.NewError("storeErrSnapshotVerify", func() string { return locales.StoreErrSnapshotVerify(name) })
	}
	return dst, nil
}

// freshSnapshotPath 返回 snapshots/<name>/<stampName>-N 里第一个不存在的路径。
func (c *Config) freshSnapshotPath(name, stampName string) (string, error) {
	for i := 1; i < 1000; i++ {
		p := c.SnapshotPath(name, fmt.Sprintf("%s-%d", stampName, i))
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p, nil
		}
	}
	return "", i18n.NewError("storeErrNoSnapshotPath", func() string { return locales.StoreErrNoSnapshotPath(name, stampName) })
}

// within 报告 p 是否在 root 之内（不含 root 本身）：两边都先解析符号链接，
// 解析不了（路径不存在等）退回绝对路径。
func within(root, p string) bool {
	resolve := func(s string) string {
		if r, err := filepath.EvalSymlinks(s); err == nil {
			s = r
		}
		if abs, err := filepath.Abs(s); err == nil {
			s = abs
		}
		return s
	}
	rel, err := filepath.Rel(resolve(root), resolve(p))
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// latestSubdir 返回 dir 下按名字排序最大的子目录名（时间戳目录名可排序）。
func latestSubdir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", i18n.Wrapf(err, "storeErrReadFile", func() string { return locales.StoreErrReadFile(dir) })
	}
	var best string
	for _, e := range entries {
		if e.IsDir() && e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return "", i18n.NewError("storeErrNoSubdir", func() string { return locales.StoreErrNoSubdir() })
	}
	return best, nil
}
