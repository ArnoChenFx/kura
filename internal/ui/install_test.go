package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

func TestParseRepo(t *testing.T) {
	cases := []struct{ in, repo, ref, sub string }{
		{"mattpocock/skills", "mattpocock/skills", "", ""},
		{"https://github.com/mattpocock/skills.git", "mattpocock/skills", "", ""},
		{"github.com/me/r/tree/dev/skills/tdd/", "me/r", "dev", "skills/tdd"},
		// blob 链接指向文件：子目录是文件所在的目录，不含 SKILL.md。
		{" https://github.com/me/r/blob/main/a/SKILL.md ", "me/r", "main", "a"},
		{"https://github.com/me/r/blob/main/skills/tdd/SKILL.md?plain=1#L3", "me/r", "main", "skills/tdd"},
		{"https://github.com/me/r/blob/main/SKILL.md", "me/r", "main", ""},
		{"https://github.com/me/r/blob/main", "me/r", "main", ""},
		{"https://github.com/me/r/tree/main/skills/tdd#readme", "me/r", "main", "skills/tdd"},
		{"onlyowner", "onlyowner", "", ""},
	}
	for _, c := range cases {
		repo, ref, sub := parseRepo(c.in)
		if repo != c.repo || ref != c.ref || sub != c.sub {
			t.Errorf("parseRepo(%q) = %q %q %q，想要 %q %q %q", c.in, repo, ref, sub, c.repo, c.ref, c.sub)
		}
	}
	s := installState{repo: "https://github.com/me/r/tree/dev/x", ref: "v1"}
	if o := s.installOptions(); o.Repo != "me/r" || o.Ref != "v1" || o.SubPath != "x" {
		t.Errorf("表单单独填的分支应优先：%+v", o)
	}
	if (&installState{repo: "onlyowner"}).installReady() {
		t.Error("不完整的仓库名不应允许安装")
	}
	// 粘贴 SKILL.md 的 blob 链接，装的是它所在的技能目录。
	b := installState{repo: "https://github.com/me/r/blob/dev/skills/tdd/SKILL.md"}
	if o := b.installOptions(); o.Repo != "me/r" || o.Ref != "dev" || o.SubPath != "skills/tdd" || !b.installReady() {
		t.Errorf("blob 链接应指向 SKILL.md 所在目录：%+v", o)
	}
}

func TestInstallOptionsLocal(t *testing.T) {
	s := installState{source: installLocal, local: " /x/y ", name: " mine ", repo: "ignored/repo"}
	o := s.installOptions()
	if o.LocalPath != "/x/y" || o.Name != "mine" || o.Repo != "" {
		t.Errorf("本地来源应忽略仓库、裁掉空白：%+v", o)
	}
	if !s.installReady() {
		t.Error("填了文件夹就可以安装")
	}
	if (&installState{source: installLocal, local: "  "}).installReady() {
		t.Error("文件夹为空白时不应允许安装")
	}
}

// TestOpenInstallKeepsInputButClearsError 验证重新打开弹窗时保留上次填写的
// 内容、清掉旧错误。
func TestOpenInstallKeepsInputButClearsError(t *testing.T) {
	a, _ := newTestApp(t)
	a.install = installState{repo: "me/r", err: "boom"}
	a.OpenInstall()
	if !a.install.open || a.install.repo != "me/r" || a.install.err != "" {
		t.Errorf("OpenInstall 后 = %+v", a.install)
	}
}

// TestInstallLocalFolder 验证本地安装的完整流程：打开弹窗 → 切到本地文件夹
// → 选择文件夹 → 安装；成功后弹窗关闭、新技能被选中并出现在卡片里。重复
// 安装同一个文件夹时报错，弹窗保持打开并显示原因。
func TestInstallLocalFolder(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "existing", nil)
	src := filepath.Join(makeRoot(t, "src", "pdf-tools"), "pdf-tools")
	a.pickFolder = func() (string, error) { return src, nil }
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	if !a.install.open || !tt.HasText("安装到 "+cfg.Root) {
		t.Fatalf("点「安装技能」应打开弹窗并显示目标目录，文本 %v", tt.Texts())
	}
	click(t, a, tt, "本地文件夹")
	if a.install.source != installLocal {
		t.Fatalf("应切到本地文件夹页签，source=%d", a.install.source)
	}
	click(t, a, tt, "选择…")
	if a.install.local != src {
		t.Fatalf("选择文件夹后应填进路径，local=%q", a.install.local)
	}

	click(t, a, tt, "确认安装")
	if a.install.open {
		t.Errorf("安装成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("pdf-tools"), "SKILL.md")); err != nil {
		t.Fatalf("技能应已复制进根目录: %v", err)
	}
	var meta *store.Meta
	for _, sk := range a.skills {
		if sk.Name == "pdf-tools" {
			meta = sk.Meta
		}
	}
	if meta == nil || meta.Source.Type != store.SourceLocal || meta.Source.Path != src {
		t.Errorf("元数据应记录本地来源 %s，得到 %+v", src, meta)
	}
	if a.selName != "pdf-tools" || a.statusErr || !tt.HasText("已安装 pdf-tools") {
		t.Errorf("新装的技能应被选中并在状态行报告：selName=%q 文本 %v", a.selName, tt.Texts())
	}
	if !tt.HasText("pdf-tools 开关") {
		t.Errorf("新技能应出现在卡片里，文本 %v", tt.Texts())
	}

	// 再装一次同一个文件夹：同名冲突。
	click(t, a, tt, "安装技能")
	click(t, a, tt, "选择…")
	click(t, a, tt, "确认安装")
	if !a.install.open || !tt.HasText("已存在同名技能") {
		t.Errorf("同名冲突时弹窗应保持打开并显示原因，文本 %v", tt.Texts())
	}
	if !a.statusErr || a.install.busy {
		t.Errorf("失败后状态行应为错误且不再忙碌：statusErr=%v busy=%v", a.statusErr, a.install.busy)
	}
	click(t, a, tt, "取消")
	if a.install.open {
		t.Error("点「取消」应关闭弹窗")
	}
}

// TestInstallGitHubFailureKeepsModalOpen 验证 GitHub 安装失败时弹窗保持打开
// 并显示原因。PATH 指向空目录，git 必然找不到，因此既不联网也不依赖本机配置。
func TestInstallGitHubFailureKeepsModalOpen(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "me/repo"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if !a.install.open || !tt.HasText("git clone 失败") {
		t.Errorf("失败时弹窗应保持打开并显示原因，文本 %v", tt.Texts())
	}
	if !a.statusErr || a.install.busy {
		t.Errorf("失败后状态行应为错误且不再忙碌：statusErr=%v busy=%v", a.statusErr, a.install.busy)
	}
	if entries, _ := os.ReadDir(cfg.Root); len(entries) != 0 {
		t.Errorf("失败的安装不应留下任何文件夹，得到 %v", entries)
	}
}

// TestInstallNeedsSource 验证来源没填够时「安装」不可用，点了也不会开始。
func TestInstallNeedsSource(t *testing.T) {
	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)
	spawned := 0
	orig := a.spawn
	a.spawn = func(fn func()) { spawned++; orig(fn) }

	click(t, a, tt, "安装技能")
	click(t, a, tt, "确认安装")
	if spawned != 0 || a.install.busy || !a.install.open {
		t.Errorf("没填来源时点「安装」不应开始：spawned=%d busy=%v open=%v", spawned, a.install.busy, a.install.open)
	}
}

// TestPickInstallFolderErrorIsShown 验证选择文件夹出错时弹窗显示原因。
func TestPickInstallFolderErrorIsShown(t *testing.T) {
	a, _ := newTestApp(t)
	a.pickFolder = func() (string, error) { return "", errors.New("没有权限") }
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	click(t, a, tt, "本地文件夹")
	click(t, a, tt, "选择…")
	if !tt.HasText("选择文件夹失败: 没有权限") || a.install.picking {
		t.Errorf("应显示选择失败的原因且恢复可用，picking=%v 文本 %v", a.install.picking, tt.Texts())
	}
}
