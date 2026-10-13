package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// maxFrontmatterLines 限制读取 SKILL.md 头部的行数：description 总在
// frontmatter 里，不必读完整个文件。
const maxFrontmatterLines = 200

// readDescription 从 SKILL.md 的 YAML frontmatter 里取 description 字段，
// 用于界面上的卡片摘要。只支持常见写法：单行值（可带引号）与 `>`/`|`
// 折叠块。文件缺失、没有 frontmatter 或没有该字段时返回空串，绝不报错。
func readDescription(dir string) string {
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
		key, val, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "description" || key != strings.TrimLeft(key, " \t") {
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
