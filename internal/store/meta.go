package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Source 描述技能的安装来源。
// Type 为 "github" 时填写 URL/Ref/SubPath/Commit；Type 为 "local" 时填写 Path。
type Source struct {
	Type    string `json:"type"`
	URL     string `json:"url,omitempty"`
	Ref     string `json:"ref,omitempty"`
	SubPath string `json:"subPath,omitempty"`
	Commit  string `json:"commit,omitempty"`
	// Path 仅在 Type=local 时使用，记录来源目录（绝对路径）。
	Path string `json:"path,omitempty"`
}

// Meta 是技能文件夹内 .kura-meta.json 的完整内容，字段与 REQUIREMENTS.md
// 中的示例一一对应（metaVersion 恒为 1）。
type Meta struct {
	MetaVersion int    `json:"metaVersion"`
	ID          string `json:"id"`
	// DisplayName 默认与 id 相同。
	DisplayName string `json:"displayName"`
	Source      Source `json:"source"`
	// PluginName 来自 ~/.agents/.skill-lock.json（若技能列在其中）。
	PluginName  string `json:"pluginName,omitempty"`
	InstalledAt string `json:"installedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// MetaVersion 是当前写入的元数据版本。
const MetaVersion = 1

// metaReadResult 包装一次 .kura-meta.json 读取的结果。
// 文件不存在：Found=false，Err=nil；
// 存在且正常：Found=true，Meta 非 nil；
// 存在但损坏：Found=true，Err 非 nil，Meta=nil（由调用方降级，绝不 panic）。
type metaReadResult struct {
	Found bool
	Err   error
	Meta  *Meta
}

// readMeta 读取 dir/.kura-meta.json，结果语义见 metaReadResult。
func readMeta(dir string) metaReadResult {
	path := filepath.Join(dir, MetaFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metaReadResult{Found: false}
		}
		return metaReadResult{Found: true, Err: fmt.Errorf("store: 读取 %s 失败: %w", path, err)}
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return metaReadResult{Found: true, Err: fmt.Errorf("store: 解析 %s 失败（JSON 损坏）: %w", path, err)}
	}
	return metaReadResult{Found: true, Meta: &m}
}

// writeMeta 把 m 写入 dir/.kura-meta.json（缩进两格）。写入前固化 metaVersion=1。
func writeMeta(dir string, m *Meta) error {
	if m == nil {
		return errors.New("store: 不能写入空元数据")
	}
	m.MetaVersion = MetaVersion
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("store: 序列化元数据失败: %w", err)
	}
	path := filepath.Join(dir, MetaFileName)
	// 末尾补一个换行，符合常见文本文件习惯。
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("store: 写入 %s 失败: %w", path, err)
	}
	return nil
}
