package cli

// 输出与错误处理的共享辅助：JSON 形状定义、编码、错误输出、用法错误。
// 被 commands.go 与 doctor.go 共同使用。

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// resultJSON 是 disable/enable/remove/restore/add/update --json 的顶层形状，
// 直接对应 store.Result（snapshotDir 仅有快照操作才有）。
type resultJSON struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	Dir         string `json:"dir"`
	SnapshotDir string `json:"snapshotDir,omitempty"`
}

// errorJSON 是 --json 模式下错误信息在 stderr 上的形状。
type errorJSON struct {
	Error string `json:"error"`
}

// writeJSON 把 v 以缩进两格的 JSON 写入 w（末尾补换行）。
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("cli: JSON 编码失败: %w", err)
	}
	return nil
}

// writeJSONOrErr 输出 JSON；编码失败按内部错误处理（理论不可达，防御分支）。
func writeJSONOrErr(w io.Writer, v any) int {
	if err := writeJSON(w, v); err != nil {
		fmt.Fprintf(io.Discard, "%v", err)
		return ExitError
	}
	return ExitOK
}

// usageError 统一处理 flag 解析失败：flag 包已把详情写进 stderr。
// --json 模式下 flag 包输出的是纯文本，额外追加一条结构化错误，
// 保证脚本消费方在 stderr 上总能拿到 {"error": ...}。
func usageError(stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK // 用户主动请求 -h，不算错误。
	}
	fmt.Fprintf(stderr, "kura: 参数错误: %v（用 kura --help 查看用法）\n", err)
	_ = writeJSON(stderr, errorJSON{Error: "参数错误: " + err.Error()})
	return ExitUsage
}
