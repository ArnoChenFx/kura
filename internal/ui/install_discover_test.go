package ui

// 安装弹窗的「自动发现仓库内技能」测试：多候选时弹窗列出候选并能让用户
// 选中安装；唯一候选直接装；仓库没有技能时显示原因。全部仓库都在
// t.TempDir() 里本地构造，clone 经 insteadOf 重写指向本地 bare（不联网，
// 不碰用户的 git 配置与真实技能目录）。

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// seedRepo 在 base 下构造技能仓库并提交；dirs 是技能目录（slash 相对路径）。
// manifest 非 nil 时写入 .claude-plugin/plugin.json。
func seedRepo(t *testing.T, base string, manifest string, dirs ...string) string {
	t.Helper()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		p := filepath.Join(repo, filepath.FromSlash(d))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(d)
		if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\n# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if manifest != "" {
		if err := os.MkdirAll(filepath.Join(repo, ".claude-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 占位文件：保证空仓库也有内容可提交，且 HEAD 存在（clone --branch
	// main 需要分支存在；不同 git 版本 init 后的默认分支名不同）。
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# 测试仓库\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	// HEAD 指到 main：clone --branch main 与 blob 链接里的 ref 都需要
	// 分支存在（init 默认分支名随 git 版本不同，symbolic-ref 到处可用）。
	git("symbolic-ref", "HEAD", "refs/heads/main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	return repo
}

// mirrorAlias 镜像 repo 为 bare，并把 https://github.com/<alias>.git 的
// clone 重写过去（隔离的临时全局配置，t.Cleanup 自动注销）。
func mirrorAlias(t *testing.T, repo, alias string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bare := filepath.Join(t.TempDir(), "bare.git")
	git("clone", "-q", "--bare", repo, bare)
	key := "url." + bare + ".insteadOf"
	git("config", "--global", key, "https://github.com/"+alias+".git")
	t.Cleanup(func() { exec.Command("git", "config", "--global", "--unset", key).Run() }) //nolint:errcheck
}

// TestInstallModalCandidatesFlow 端到端验证多候选流程：填 repo → 确认安装
// → 弹窗保持打开并出现候选列表 → 点击候选填进子目录 → 再次确认安装 →
// 装上并选中。中途核对：候选未选时子目录仍为空。
func TestInstallModalCandidatesFlow(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "",
		"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me")
	mirrorAlias(t, repo, "mattpocock/skills")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "mattpocock/skills"
	refresh(a, tt)

	// 第一次点安装：子目录为空 → 走发现 → 弹窗保持打开、列出 3 个候选。
	click(t, a, tt, "确认安装")
	if !a.install.open || a.install.busy {
		t.Fatalf("发现期间弹窗应保持打开：open=%v busy=%v", a.install.open, a.install.busy)
	}
	if len(a.install.cands) != 3 {
		t.Fatalf("应列出 3 个候选，得到 %v", a.install.cands)
	}
	for _, want := range []string{"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me"} {
		if !tt.HasText(want) {
			t.Errorf("候选 %q 应出现在弹窗里，文本 %v", want, tt.Texts())
		}
	}

	// 点击候选：填进子目录、清掉候选列表。
	click(t, a, tt, "skills/engineering/tdd")
	if a.install.subPath != "skills/engineering/tdd" || len(a.install.cands) != 0 {
		t.Fatalf("点击候选应填进子目录并收起列表：subPath=%q cands=%v", a.install.subPath, a.install.cands)
	}

	// 再次确认安装：装上、弹窗关闭、新技能被选中。
	click(t, a, tt, "确认安装")
	if a.install.open {
		t.Errorf("安装成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("tdd"), "SKILL.md")); err != nil {
		t.Fatalf("tdd 应已装进根目录: %v", err)
	}
	if a.selName != "tdd" || a.statusErr {
		t.Errorf("新装的技能应被选中且状态行无错误：selName=%q statusErr=%v", a.selName, a.statusErr)
	}
}

// TestInstallModalSingleCandidateAutoInstalls 验证唯一候选（清单声明的
// 一个技能）时点一次「安装」就装上，不需要用户再选。
func TestInstallModalSingleCandidateAutoInstalls(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), `{"skills":["./skills/solo"]}`, "skills/solo")
	mirrorAlias(t, repo, "owner/solo-repo")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/solo-repo"
	refresh(a, tt)
	click(t, a, tt, "确认安装")

	if a.install.open {
		t.Errorf("唯一候选应直接装上并关闭弹窗，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("solo"), "SKILL.md")); err != nil {
		t.Fatalf("solo 应已装进根目录: %v", err)
	}
}

// TestInstallModalNoSkillsShowsError 验证仓库里没有技能时弹窗显示原因并
// 保持打开（与其它安装失败一致）。
func TestInstallModalNoSkillsShowsError(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "") // 只有 git 仓库，没有技能
	mirrorAlias(t, repo, "owner/empty")

	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/empty"
	refresh(a, tt)
	click(t, a, tt, "确认安装")

	if !a.install.open || !tt.HasText("仓库内未找到含 SKILL.md 的技能目录") {
		t.Errorf("零候选应报错且弹窗保持打开，文本 %v", tt.Texts())
	}
	if !a.statusErr || a.install.busy {
		t.Errorf("状态行应为错误且不再忙碌：statusErr=%v busy=%v", a.statusErr, a.install.busy)
	}
}

// TestRenderInstallCandidatesShot 真实渲染安装弹窗的候选选择状态并保存
// PNG（照 shots_test.go 的模式）：设 KURA_UI_SHOTS=<目录> 时才跑，CI 跳过。
// 数据直接填进 installState（不联网、不碰真实目录）。
func TestRenderInstallCandidatesShot(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过安装弹窗截图")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	a.install = installState{
		open: true, source: installGitHub, repo: "mattpocock/skills",
		cands: []string{
			"skills/engineering/ask-matt", "skills/engineering/code-review",
			"skills/engineering/codebase-design", "skills/engineering/diagnosing-bugs",
			"skills/engineering/domain-modeling", "skills/engineering/grill-with-docs",
			"skills/engineering/implement", "skills/engineering/pr",
			"skills/engineering/tdd", "skills/productivity/grill-me",
			"skills/productivity/grilling", "skills/productivity/handoff",
		},
	}
	tt := newTester(a, 1140, 760)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()
	img := tt.Image()
	f, err := os.Create(filepath.Join(out, "install-candidates.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestInstallExplicitSubPathSkipsDiscovery 验证从 tree/blob 链接解析出的
// 子目录等同用户指定：直接按它安装，不走发现（不被自动发现覆盖）。
func TestInstallExplicitSubPathSkipsDiscovery(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "",
		"skills/engineering/tdd", "skills/engineering/code-review")
	mirrorAlias(t, repo, "owner/link-repo")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	// 粘贴 SKILL.md 的 blob 链接：parseRepo 会解析出 repo/ref/子目录。
	a.install.repo = "https://github.com/owner/link-repo/blob/main/skills/engineering/code-review/SKILL.md"
	refresh(a, tt)
	click(t, a, tt, "确认安装")

	if a.install.open {
		t.Errorf("链接里带了子目录应直接安装并关闭弹窗，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("code-review"), "SKILL.md")); err != nil {
		t.Fatalf("code-review 应已装进根目录: %v", err)
	}
}
