// 演示「仓库内技能发现」的三种情形（任务书要求的证据 5）。
// 调用 internal/store 的真实 API，在临时目录里构造假仓库（不联网、不碰
// 用户的真实目录），打印目录树与发现结果。在 worktree 根目录运行：
//
//	go run ./tools/demo-discover
package main

// demo 只演示 DiscoverSkills 的三种情形；clone / 安装路径的端到端验证
// 由单元测试与真机验证覆盖，这里不复刻任何逻辑。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
)

// writeSkill 在 root 下的 slash 相对目录写一个最小技能。
func writeSkill(root, rel string) {
	p := filepath.Join(root, filepath.FromSlash(rel), "SKILL.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("---\nname: "+filepath.Base(rel)+"\n---\n\n# "+filepath.Base(rel)+"\n"), 0o644) //nolint:errcheck
}

// writeFile 在 root 下按 slash 相对路径写文件。
func writeFile(root, rel, content string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(content), 0o644) //nolint:errcheck
}

// tree 打印目录树（两格缩进，目录带 / 后缀，跳过 .git）。
func tree(root string) {
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error { //nolint:errcheck
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		fmt.Printf("%s%s\n", strings.Repeat("  ", depth), name)
		return nil
	})
}

func main() {
	tmp, err := os.MkdirTemp("", "kura-demo-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	// 情形 1：唯一技能直接装 —— 仓库里只有一个技能目录。
	r1 := filepath.Join(tmp, "single-repo")
	writeSkill(r1, "skills/only/lonely")
	dr1, err1 := store.DiscoverSkills(r1)
	fmt.Println("== 情形 1：唯一技能 ==")
	tree(r1)
	fmt.Printf("DiscoverSkills → paths=%v manifest=%v err=%v\n\n", dr1.Paths, dr1.Manifest, err1)

	// 情形 2：多技能列候选 —— mattpocock/skills 的三层布局。
	r2 := filepath.Join(tmp, "multi-repo")
	for _, p := range []string{
		"skills/engineering/tdd", "skills/engineering/code-review",
		"skills/productivity/grill-me", "skills/misc/one-off",
	} {
		writeSkill(r2, p)
	}
	writeFile(r2, "node_modules/fake/SKILL.md", "node_modules 不应被发现")
	dr2, err2 := store.DiscoverSkills(r2)
	fmt.Println("== 情形 2：多技能（三层布局）==")
	tree(r2)
	fmt.Printf("DiscoverSkills → paths=%v manifest=%v err=%v\n", dr2.Paths, dr2.Manifest, err2)
	ce := &store.CandidatesError{Repo: "mattpocock/skills", Paths: dr2.Paths}
	fmt.Printf("多候选时 Add 返回的错误信息:\n%s\n\n", ce.Error())

	// 情形 3：plugin.json 清单优先 —— 扫描能看到草稿，清单排除它。
	r3 := filepath.Join(tmp, "manifest-repo")
	writeSkill(r3, "skills/published/alpha")
	writeSkill(r3, "skills/published/gamma")
	writeSkill(r3, "skills/in-progress/draft")
	writeFile(r3, ".claude-plugin/plugin.json",
		`{"name":"x","skills":["./skills/published/alpha","./skills/published/gamma"]}`)
	dr3, err3 := store.DiscoverSkills(r3)
	fmt.Println("== 情形 3：plugin.json 清单优先 ==")
	tree(r3)
	fmt.Printf("DiscoverSkills → paths=%v manifest=%v err=%v\n", dr3.Paths, dr3.Manifest, err3)
}
