package store

// add_by_name.go 的测试：按技能名从（本地假）GitHub 仓库安装。
//
// 全部仓库都在 t.TempDir() 里构造，经 installMirror 的 insteadOf 冒充 github
// 地址，不联网，不碰真实的 ~/.agents/skills 与 ~/.kura。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddSkillByNameInstallsWithMeta 验证按目录名找到技能、装成市场上的名字，
// 元数据记下来源、commit 与子目录树哈希（与 Add 一致，之后才能检查更新），
// 且只装这一个技能，同仓库里的其他技能不被带进来。
func TestAddSkillByNameInstallsWithMeta(t *testing.T) {
	c := newTestConfig(t)
	multiSkillRepo(t, "owner/mkt", "skills/tdd", "skills/other")
	res, err := c.AddSkillByName("owner/mkt", "tdd")
	if err != nil {
		t.Fatalf("AddSkillByName: %v", err)
	}
	if res.ID != "tdd" {
		t.Fatalf("装成了 %q，想要 tdd", res.ID)
	}
	if _, err := os.Stat(filepath.Join(c.SkillPath("tdd"), "SKILL.md")); err != nil {
		t.Fatalf("技能内容没有装上: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.SkillPath("tdd"), ".git")); !os.IsNotExist(err) {
		t.Errorf("clone 的 .git 不应落进技能目录: %v", err)
	}
	if _, err := os.Stat(c.SkillPath("other")); !os.IsNotExist(err) {
		t.Errorf("同仓库的其他技能不应被装上: %v", err)
	}
	m := readMeta(c.SkillPath("tdd")).Meta
	if m == nil {
		t.Fatal("没有写入元数据")
	}
	src := m.Source
	if src.Type != SourceGitHub || src.URL != "https://github.com/owner/mkt.git" ||
		src.SubPath != "skills/tdd" || src.Commit == "" || src.Tree == "" {
		t.Errorf("来源元数据不对: %+v", src)
	}
}

// TestAddSkillByNameMatchesFrontmatterNameFirst 验证 SKILL.md 写了 name 的技能
// 只按 name 比（与 CLI 的 --skill 一致）：目录名 write-a-prd 的技能 name 是别的，
// 不能因为目录名对上就被拉进来；按 name 找到的是目录名不同的那一个。
func TestAddSkillByNameMatchesFrontmatterNameFirst(t *testing.T) {
	c := newTestConfig(t)
	repo := t.TempDir()
	writeRepoFile(t, repo, "skills/write-a-prd/SKILL.md", "---\nname: something-else\n---\n\n# 别的技能\n")
	writeRepoFile(t, repo, "skills/write-prd/SKILL.md", "---\nname: write-a-prd\n---\n\n# 写 PRD\n")
	writeRepoFile(t, repo, "README.md", "仓库说明\n")
	commitRepo(t, repo)
	restore := installMirror(t, repo, "owner/fm")
	t.Cleanup(restore)

	res, err := c.AddSkillByName("owner/fm", "Write-A-PRD")
	if err != nil {
		t.Fatalf("AddSkillByName: %v", err)
	}
	if res.ID != "Write-A-PRD" {
		t.Fatalf("装成了 %q", res.ID)
	}
	m := readMeta(c.SkillPath("Write-A-PRD")).Meta
	if m == nil || m.Source.SubPath != "skills/write-prd" {
		t.Errorf("应按 frontmatter 的 name 找到 skills/write-prd，得到 %+v", m)
	}
}

// TestAddSkillByNameDirNameWhenNoFrontmatterName 验证没写 name 的技能按目录名找。
func TestAddSkillByNameDirNameWhenNoFrontmatterName(t *testing.T) {
	c := newTestConfig(t)
	repo := t.TempDir()
	writeRepoFile(t, repo, "skills/plain/SKILL.md", "# 没有 frontmatter\n")
	writeRepoFile(t, repo, "README.md", "仓库说明\n")
	commitRepo(t, repo)
	restore := installMirror(t, repo, "owner/plain")
	t.Cleanup(restore)

	res, err := c.AddSkillByName("owner/plain", "plain")
	if err != nil {
		t.Fatalf("AddSkillByName: %v", err)
	}
	if m := readMeta(c.SkillPath(res.ID)).Meta; m == nil || m.Source.SubPath != "skills/plain" {
		t.Errorf("应按目录名找到 skills/plain，得到 %+v", m)
	}
}

// TestAddSkillByNameRepoRootSkill 验证技能就是仓库根：按 frontmatter 的 name 找，
// 来源里的子目录为空（与 Add 的根技能一致）。
func TestAddSkillByNameRepoRootSkill(t *testing.T) {
	c := newTestConfig(t)
	rootSkillRepo(t, "owner/rootskill", "---\nname: rootskill\n---\n\n# 根技能\n")
	res, err := c.AddSkillByName("owner/rootskill", "rootskill")
	if err != nil {
		t.Fatalf("AddSkillByName: %v", err)
	}
	m := readMeta(c.SkillPath(res.ID)).Meta
	if res.ID != "rootskill" || m == nil || m.Source.SubPath != "" || m.Source.Tree == "" {
		t.Errorf("根技能安装不对：id=%q meta=%+v", res.ID, m)
	}
}

// TestAddSkillByNameRepoRootFallsBackToRepoName 验证根技能没有 name 时，
// 名字取仓库名（临时 clone 目录名是随机的，不能当技能名）。
func TestAddSkillByNameRepoRootFallsBackToRepoName(t *testing.T) {
	c := newTestConfig(t)
	rootSkillRepo(t, "owner/repo-named", "# 没有 frontmatter\n")
	res, err := c.AddSkillByName("owner/repo-named", "repo-named")
	if err != nil {
		t.Fatalf("AddSkillByName: %v", err)
	}
	if res.ID != "repo-named" {
		t.Errorf("装成了 %q，想要仓库名 repo-named", res.ID)
	}
}

// TestAddSkillByNameNotInRepo 验证仓库里没有这个技能：哨兵可判、报错带上仓库与名字，
// 什么都不装。
func TestAddSkillByNameNotInRepo(t *testing.T) {
	c := newTestConfig(t)
	multiSkillRepo(t, "owner/miss", "skills/tdd")
	_, err := c.AddSkillByName("owner/miss", "nope")
	if !errors.Is(err, ErrSkillNotInRepo) {
		t.Fatalf("应为 ErrSkillNotInRepo，得到 %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "nope") || !strings.Contains(msg, "owner/miss") {
		t.Errorf("报错应带上技能名与仓库：%s", msg)
	}
	if _, statErr := os.Stat(c.SkillPath("nope")); !os.IsNotExist(statErr) {
		t.Error("失败时不应留下任何技能目录")
	}
}

// TestAddSkillByNameAmbiguousReturnsCandidates 验证同名技能有多个时交还候选，
// 不替用户挑一个。
func TestAddSkillByNameAmbiguousReturnsCandidates(t *testing.T) {
	c := newTestConfig(t)
	multiSkillRepo(t, "owner/dup", "a/tdd", "b/tdd")
	_, err := c.AddSkillByName("owner/dup", "tdd")
	var ce *CandidatesError
	if !errors.As(err, &ce) {
		t.Fatalf("应为 *CandidatesError，得到 %v", err)
	}
	if len(ce.Paths) != 2 || ce.Paths[0] != "a/tdd" || ce.Paths[1] != "b/tdd" {
		t.Errorf("候选不对：%v", ce.Paths)
	}
	if _, statErr := os.Stat(c.SkillPath("tdd")); !os.IsNotExist(statErr) {
		t.Error("有歧义时不应装任何技能")
	}
}

// TestAddSkillByNameNameClash 验证目标目录已有同名技能时报错，原技能不被覆盖。
func TestAddSkillByNameNameClash(t *testing.T) {
	c := newTestConfig(t)
	multiSkillRepo(t, "owner/clash", "skills/tdd")
	if _, err := c.AddSkillByName("owner/clash", "tdd"); err != nil {
		t.Fatalf("首次安装: %v", err)
	}
	if _, err := c.AddSkillByName("owner/clash", "tdd"); err == nil {
		t.Fatal("同名技能应报错")
	}
	if _, err := os.Stat(filepath.Join(c.SkillPath("tdd"), "SKILL.md")); err != nil {
		t.Errorf("原技能被破坏了: %v", err)
	}
}

// TestAddSkillByNameRejectsBadInput 验证非法的仓库地址与空名字不触发任何 clone。
func TestAddSkillByNameRejectsBadInput(t *testing.T) {
	c := newTestConfig(t)
	cloned := 0
	cloneHook = func(string) { cloned++ }
	t.Cleanup(func() { cloneHook = nil })
	for _, tc := range []struct{ repo, name string }{
		{"noslash", "tdd"},
		{"a/b/c", "tdd"},
		{"owner/repo", "  "},
	} {
		if _, err := c.AddSkillByName(tc.repo, tc.name); err == nil {
			t.Errorf("AddSkillByName(%q, %q) 应失败", tc.repo, tc.name)
		}
	}
	if cloned != 0 {
		t.Errorf("非法输入不应触发 clone，实际 %d 次", cloned)
	}
}
