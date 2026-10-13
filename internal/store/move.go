package store

import (
	"fmt"
	"os"
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
func moveDir(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	// 跨卷回退：staging 目录与 dst 同卷，保证最后一步 Rename 原子。
	staging := dst + moveTmpSuffix
	// 清掉上次崩溃可能遗留的同名 staging，避免复制进脏目录。
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("store: 清理跨卷移动的临时目录 %s 失败: %w", staging, err)
	}
	if err := copyDir(src, staging); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("store: 跨卷移动 %s：复制到 %s 失败（原目录未改动）: %w", src, staging, err)
	}
	if err := os.Rename(staging, dst); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("store: 跨卷移动 %s：落位 %s 失败（原目录未改动）: %w", src, dst, err)
	}
	// 复制与落位都成功后才删源目录。这一步失败不回滚（数据已在 dst），
	// 只把残留状态说清楚，由调用方报给用户。
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("store: 跨卷移动完成，但删除源目录 %s 失败（内容已在 %s）: %w", src, dst, err)
	}
	return nil
}

// moveTmpSuffix 是跨卷回退时 staging 目录的后缀，与 dst 同卷同父目录。
const moveTmpSuffix = ".kura-move-tmp"
