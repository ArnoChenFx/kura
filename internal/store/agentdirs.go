package store

// agentdirs.go — 本机常用 agent 框架的全局技能目录（自动发现的候选，需求第 8 条）。
//
// 只做只读的磁盘检查：列出约定的全局目录，返回其中真的存在的文件夹。是否
// 加入列表、何时加入、怎样记住已考虑过的路径，由 Library.DiscoverAgentRoots
// 负责。

import (
	"os"
	"path/filepath"
)

// agentSkillDirs 是各 agent 框架约定的全局技能目录，相对用户主目录，斜杠分隔。
// 路径取自 vercel-labs/skills 的「Supported Agents」表（Global Path 一列）；
// 框架改了约定时改这里即可。默认根目录 ~/.agents/skills 不在表里：它本来就
// 在列表里，Cline 等框架也共用它。
var agentSkillDirs = []string{
	".claude/skills",             // Claude Code
	".codex/skills",              // Codex CLI
	".cursor/skills",             // Cursor
	".config/opencode/skills",    // OpenCode
	".gemini/skills",             // Gemini CLI
	".gemini/antigravity/skills", // Antigravity
	".copilot/skills",            // GitHub Copilot
	".pi/agent/skills",           // Pi
	".kiro/skills",               // Kiro CLI
	".qwen/skills",               // Qwen Code
	".codeium/windsurf/skills",   // Windsurf
	".roo/skills",                // Roo Code
	".trae/skills",               // Trae
	".config/goose/skills",       // Goose
	".factory/skills",            // Droid
	".augment/skills",            // Augment
	".config/agents/skills",      // 通用目录（Amp、Kimi Code CLI 等）
}

// AgentSkillDirs 返回 home 下真实存在的 agent 技能目录（绝对路径，按表中顺序）。
// 不存在、不是文件夹的路径跳过；读取失败同样当作不存在，不返回错误。home 为
// 空时返回 nil：相对路径会按进程的当前目录去找，那不是用户的主目录。
func AgentSkillDirs(home string) []string {
	if home == "" {
		return nil
	}
	var out []string
	for _, rel := range agentSkillDirs {
		dir := filepath.Join(home, filepath.FromSlash(rel))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			out = append(out, dir)
		}
	}
	return out
}
