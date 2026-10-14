package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// maxFrontmatterLines 限制读取 SKILL.md 头部的行数：description 与 name 总在
// frontmatter 里，不必读完整个文件。
const maxFrontmatterLines = 200

// readDescription 从 SKILL.md 的 YAML frontmatter 里取 description 字段，
// 用于界面上的卡片摘要。
func readDescription(dir string) string { return readFrontmatterField(dir, "description") }

// readSkillName 从 SKILL.md 的 YAML frontmatter 里取 name 字段：技能自己声明的
// 名字，安装时作为默认技能名（见 installDir）。它与 description 同属
// frontmatter，共用 readFrontmatterField 的那一套扫描规则，不另写 YAML 解析。
func readSkillName(dir string) string { return readFrontmatterField(dir, "name") }

// readFrontmatterField 从 SKILL.md 的 YAML frontmatter 里取顶层键 key 的值。
// 只支持常见写法：单行值（可带引号）与 `>`/`|` 折叠块（多行用空格连接）。
// 文件缺失、没有 frontmatter 或没有该字段时返回空串，绝不报错。
func readFrontmatterField(dir, key string) string {
	f, err := os.Open(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return ""
	}
	var block []string
	inBlock := false
	for n := 0; sc.Scan() && n < maxFrontmatterLines; n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			break
		}
		if inBlock {
			// 折叠块到下一个顶层键（无缩进的行）为止。
			if line != "" && line[0] != ' ' && line[0] != '\t' {
				break
			}
			if t := strings.TrimSpace(line); t != "" {
				block = append(block, t)
			}
			continue
		}
		k, val, ok := strings.Cut(line, ":")
		// 只认顶层键：缩进的（嵌套在别的键下面）不算。
		if !ok || strings.TrimSpace(k) != key || k != strings.TrimLeft(k, " \t") {
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" || strings.HasPrefix(val, ">") || strings.HasPrefix(val, "|") {
			inBlock = true
			continue
		}
		return unquote(val)
	}
	return strings.Join(block, " ")
}

// unquote 去掉 YAML 单行值两端成对的引号。
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
