package store

import (
	"os"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// moveDir 把 src 目录移动到 dst，是所有「移动技能目录」操作的统一入口
// （Enable/Disable/Remove/Restore/RemoveWithStamp）。
//
// 语义仍然是「移动」而非复制：常规路径只有一次 os.Rename，不产生全量复制。
// 但 Windows 的 os.Rename 不能跨卷（不同盘符）移动，会返回 WinError 17
// ERROR_NOT_SAME_DEVICE——用户的技能根目录常在 E: 盘而 ~/.kura 在 C: 盘，
// 此时改用「复制 + 删除」回退：
//
//  1. 先把 src 完整复制到目标同一卷上的临时目录（dst + moveTmpSuffix）；
//  2. 再用 os.Rename 把临时目录改名成 dst（同卷，原子）；
//  3. 最后删除 src。
//
// 回退过程任何一步失败都清理临时目录并返回错误；开始复制前 src 不会被改动，
// 因此失败时 src 原样保留，不丢数据。
//
// 非「跨设备」的错误（权限、路径不存在等）原样返回，不用回退掩盖真实问题。
//
// src 是链接（Windows 联接点、目录符号链接）时，移动的是链接本身：同卷的 os.Rename 就是把链接
// 换个位置，目标目录不动。跨卷时不走上面的「复制 + 删除」回退——那会把链接展开成完整的真目录，
// 惄惄把「移动」变成「复制它指向的内容」，而且最后删源只是摘掉链接；见 moveLinkAcrossVolumes。
func moveDir(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	if fi, lerr := os.Lstat(src); lerr == nil && isLinkMode(fi.Mode()) {
		return moveLinkAcrossVolumes(src, dst, fi.Mode())
	}
	// 跨卷回退：staging 目录与 dst 同卷，保证最后一步 Rename 原子。
	staging := dst + moveTmpSuffix
	// 清掉上次崩溃可能遗留的同名 staging，避免复制进脏目录。
	if err := os.RemoveAll(staging); err != nil {
		return i18n.Wrapf(err, "storeErrCleanStaging", func() string { return locales.StoreErrCleanStaging(staging) })
	}
	if err := copyDir(src, staging); err != nil {
		os.RemoveAll(staging)
		return i18n.Wrapf(err, "storeErrCrossVolumeCopy", func() string { return locales.StoreErrCrossVolumeCopy(src, staging) })
	}
	if err := os.Rename(staging, dst); err != nil {
		os.RemoveAll(staging)
		return i18n.Wrapf(err, "storeErrCrossVolumeRename", func() string { return locales.StoreErrCrossVolumeRename(src, dst) })
	}
	// 复制与落位都成功后才删源目录。这一步失败不回滚（数据已在 dst），
	// 只把残留状态说清楚，由调用方报给用户。
	if err := os.RemoveAll(src); err != nil {
		return i18n.Wrapf(err, "storeErrCrossVolumeSource", func() string { return locales.StoreErrCrossVolumeSource(src, dst) })
	}
	return nil
}

// moveLinkAcrossVolumes 把链接 src 跨卷移到 dst，保留链接语义，做不到就报明确的错，绝不默默改成复制内容：
//
//   - 符号链接：在 dst 用同一个目标重建符号链接（相对目标先换成绝对路径，否则在新位置会指到别处），
//     再摘掉源链接；摘源失败则回滚刚建的链接，源原样保留。Windows 上没有权限创建符号链接时同样报错。
//   - 联接点：标准库造不出联接点（os.Symlink 只造符号链接），直接报错，源链接不动。
//
// 任何失败都不会留下残缺：源链接保持原位，dst 不存在。
func moveLinkAcrossVolumes(src, dst string, mode os.FileMode) error {
	render := func() string { return locales.StoreErrLinkCrossVolume(src, dst) }
	if mode&os.ModeSymlink == 0 {
		return i18n.NewError("storeErrLinkCrossVolume", render)
	}
	target, err := os.Readlink(src)
	if err != nil {
		return i18n.Wrapf(err, "storeErrLinkCrossVolume", render)
	}
	if err := os.Symlink(absLinkTarget(src, target), dst); err != nil {
		return i18n.Wrapf(err, "storeErrLinkCrossVolume", render)
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dst) // 回滚：只摘掉刚建的链接
		return i18n.Wrapf(err, "storeErrLinkCrossVolume", render)
	}
	return nil
}

// moveTmpSuffix 是跨卷回退时 staging 目录的后缀，与 dst 同卷同父目录。
const moveTmpSuffix = ".kura-move-tmp"
