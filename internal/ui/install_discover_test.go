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
	"strings"
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

// candOn 返回候选 path 的勾选状态（找不到就 Fatal）。
func candOn(t *testing.T, s *installState, path string) bool {
	t.Helper()
	for _, c := range s.cands {
		if c.path == path {
			return c.on
		}
	}
	t.Fatalf("候选列表里没有 %s：%v", path, s.cands)
	return false
}

// TestInstallModalCandidatesMultiSelectFlow 端到端验证多候选流程：填 repo →
// 确认安装 → 弹窗保持打开并出现候选复选框（默认全不选、主按钮禁用）→
// 勾选一个 → 主按钮变成「安装选中的 1 个」→ 再确认 → 装上并选中。
func TestInstallModalCandidatesMultiSelectFlow(t *testing.T) {
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
		if candOn(t, &a.install, want) {
			t.Errorf("候选 %q 默认不应被勾选", want)
		}
	}
	// 一个都没勾选：主按钮禁用并写「安装选中的技能」。
	if !tt.HasText("安装选中的技能") || !tt.HasText("共 3 个技能") {
		t.Errorf("未勾选时应显示总数与「安装选中的技能」，文本 %v", tt.Texts())
	}

	// 勾选一个候选：主按钮文案跟上勾选数。
	click(t, a, tt, "skills/engineering/tdd")
	if !candOn(t, &a.install, "skills/engineering/tdd") {
		t.Fatalf("点击复选框应勾选该候选：%v", a.install.cands)
	}
	if !tt.HasText("安装选中的 1 个") {
		t.Errorf("勾选一个后主按钮应写「安装选中的 1 个」，文本 %v", tt.Texts())
	}

	// 再次确认安装：装上、弹窗关闭、新技能被选中。
	click(t, a, tt, "确认安装")
	if a.install.open {
		t.Errorf("安装成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("tdd"), "SKILL.md")); err != nil {
		t.Fatalf("tdd 应已装进根目录: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("code-review")); err == nil {
		t.Error("没勾选的候选不应被装上")
	}
	if a.selName != "tdd" || a.statusErr {
		t.Errorf("新装的技能应被选中且状态行无错误：selName=%q statusErr=%v", a.selName, a.statusErr)
	}
}

// TestInstallModalSelectAllInstallsEveryCandidate 验证「全选 / 全不选」切换与
// 一次装完多个：勾选 3 个后点一次主按钮，三个技能都装上。
func TestInstallModalSelectAllInstallsEveryCandidate(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "",
		"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me")
	mirrorAlias(t, repo, "owner/selectall")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/selectall"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 3 {
		t.Fatalf("应列出 3 个候选，得到 %v", a.install.cands)
	}

	// 全选 → 三个都勾上，按钮写「安装选中的 3 个」；再全不选回到 0 个。
	click(t, a, tt, "全选")
	if len(a.install.selected()) != 3 || !tt.HasText("安装选中的 3 个") {
		t.Errorf("全选后应勾上 3 个：%v 文本 %v", a.install.cands, tt.Texts())
	}
	click(t, a, tt, "全不选")
	if len(a.install.selected()) != 0 || !tt.HasText("安装选中的技能") {
		t.Errorf("全不选后应一个不留：%v 文本 %v", a.install.cands, tt.Texts())
	}

	click(t, a, tt, "全选")
	click(t, a, tt, "确认安装")
	if a.install.open {
		t.Errorf("全部成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	for _, name := range []string{"code-review", "tdd", "grill-me"} {
		if _, err := os.Stat(filepath.Join(cfg.SkillPath(name), "SKILL.md")); err != nil {
			t.Errorf("%s 应已装进根目录: %v", name, err)
		}
	}
	// 状态行给「装了几个」的摘要，选中最后一个装上的技能。
	if !tt.HasText("已安装 3 个技能") || a.statusErr {
		t.Errorf("状态行应报告装了 3 个：status=%q statusErr=%v", a.status, a.statusErr)
	}
	if a.selName != "grill-me" {
		t.Errorf("应选中最后一个装上的技能 grill-me，得到 %q", a.selName)
	}
}

// TestInstallModalPartialFailureShowsWhichFailed 验证批量安装里单项失败不影响
// 其他项：一个同名冲突，另一个照装；失败明细留在弹窗里、状态行给摘要。
func TestInstallModalPartialFailureShowsWhichFailed(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	mirrorAlias(t, repo, "owner/partial")

	a, cfg := newTestApp(t)
	// 根目录里先放一个同名技能：装 beta 时必然撞名。
	seedSkill(t, cfg, "beta", nil)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/partial"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	click(t, a, tt, "全选")
	click(t, a, tt, "确认安装")

	if !a.install.open {
		t.Fatalf("有失败时弹窗应保持打开，文本 %v", tt.Texts())
	}
	// 装成功的 alpha 落地，失败的 beta 保持原样（还是预置的那份）。
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md")); err != nil {
		t.Errorf("alpha 应已装上: %v", err)
	}
	// 失败明细：看得出是哪个技能、为什么。
	if !tt.HasText("skills/beta") || !tt.HasText("已存在同名技能") {
		t.Errorf("弹窗应显示失败的是哪个技能与原因，err=%q 文本 %v", a.install.err, tt.Texts())
	}
	if !strings.Contains(a.install.err, "skills/beta") || !strings.Contains(a.install.err, "已存在同名技能") {
		t.Errorf("install.err 应含子目录与原因，得到 %q", a.install.err)
	}
	// 状态行摘要 + 只留下失败项（未勾选）。
	if !tt.HasText("已安装 1 个，1 个失败") || !a.statusErr {
		t.Errorf("状态行应给摘要：status=%q statusErr=%v", a.status, a.statusErr)
	}
	if len(a.install.cands) != 1 || a.install.cands[0].path != "skills/beta" || a.install.cands[0].on {
		t.Errorf("失败后应只留下未勾选的失败候选：%v", a.install.cands)
	}
	if a.install.busy {
		t.Errorf("失败后不应还在忙碌：busy=%v", a.install.busy)
	}
}

// TestInstallModalSingleCheckedCandidateUsesInstallName 验证勾选一个候选时
// 「安装名称」输入框仍然管用（批量 API 的 AddSpec.Name）。
func TestInstallModalSingleCheckedCandidateUsesInstallName(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	mirrorAlias(t, repo, "owner/single-name")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/single-name"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 {
		t.Fatalf("应列出 2 个候选，得到 %v", a.install.cands)
	}
	// 勾选一个并在「安装名称」里填一个名字（测试直接写状态，等于用户输入）。
	a.install.name = "renamed"
	refresh(a, tt)
	click(t, a, tt, "skills/alpha")
	click(t, a, tt, "确认安装")

	if a.install.open {
		t.Errorf("安装成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("renamed"), "SKILL.md")); err != nil {
		t.Errorf("勾选一个时安装名称应生效（装成 renamed）: %v", err)
	}
	if a.selName != "renamed" {
		t.Errorf("应选中装成的新名字，得到 %q", a.selName)
	}
}

// TestInstallModalManualSubPathWinsOverCandidates 验证候选列表出现后手填
// 子目录仍然装那一个（这条既有路径的行为不变），勾选项不参与。
func TestInstallModalManualSubPathWinsOverCandidates(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	mirrorAlias(t, repo, "owner/manual")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "安装技能")
	a.install.repo = "owner/manual"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 {
		t.Fatalf("应列出 2 个候选，得到 %v", a.install.cands)
	}
	// 勾一个、同时手填另一个子目录：手填的优先，主按钮文案回到「安装」。
	click(t, a, tt, "skills/alpha")
	a.install.subPath = "skills/beta"
	refresh(a, tt)
	if got := installButtonLabel(&a.install); got != "安装" {
		t.Errorf("手填子目录时主按钮应是「安装」，得到 %q", got)
	}
	click(t, a, tt, "确认安装")

	if _, err := os.Stat(filepath.Join(cfg.SkillPath("beta"), "SKILL.md")); err != nil {
		t.Errorf("应装手填子目录的那个技能: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("alpha")); err == nil {
		t.Error("勾选的候选不应被装（手填子目录优先）")
	}
}

// TestInstallSelectedShowsBusyWhileInstalling 验证批量安装期间 busy 语义与单个
// 安装一致：spinner 文案显示、主按钮不可用（canInstall 为 false）、后台只发起
// 一个任务。用「扣住 spawn」的方式在后台任务真正跑之前检查这个中间状态。
func TestInstallSelectedShowsBusyWhileInstalling(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	mirrorAlias(t, repo, "owner/busy")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/busy"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	click(t, a, tt, "全选")

	var held []func()
	orig := a.spawn
	a.spawn = func(fn func()) { held = append(held, fn) }
	click(t, a, tt, "确认安装")
	if !a.install.busy || a.install.canInstall() {
		t.Errorf("安装期间应处于忙碌且主按钮不可用：busy=%v canInstall=%v", a.install.busy, a.install.canInstall())
	}
	if a.status != "正在安装…" || !tt.HasText("正在安装…") {
		t.Errorf("安装期间应显示进行中的文案：status=%q 文本 %v", a.status, tt.Texts())
	}
	if len(held) != 1 {
		t.Fatalf("应只发起一个后台任务（批量），得到 %d", len(held))
	}
	// 放行任务并回放 update：两个技能都装上、弹窗关闭、退出忙碌。
	a.spawn = orig
	held[0]()
	refresh(a, tt)
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(cfg.SkillPath(name), "SKILL.md")); err != nil {
			t.Errorf("%s 应已装上: %v", name, err)
		}
	}
	if a.install.busy || a.install.open {
		t.Errorf("装完应退出忙碌并关闭弹窗：busy=%v open=%v", a.install.busy, a.install.open)
	}
}

// mirrorAliasPair 把两个仓库各自镜像成 bare，并把两条 github 地址→本地 bare 的
// insteadOf 规则写进**同一个**隔离 git 配置。不能直接调两次 mirrorAlias：第二次
// 会把 GIT_CONFIG_GLOBAL 换到一个新的空文件，第一条规则就没了（写回归测试时
// 踩到过）——同一时刻生效的规则必须落在同一份配置里。
func mirrorAliasPair(t *testing.T, repoA, aliasA, repoB, aliasB string) {
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
	for _, m := range []struct{ repo, alias string }{{repoA, aliasA}, {repoB, aliasB}} {
		bare := filepath.Join(t.TempDir(), "bare.git")
		git("clone", "-q", "--bare", m.repo, bare)
		key := "url." + bare + ".insteadOf"
		git("config", "--global", key, "https://github.com/"+m.alias+".git")
		t.Cleanup(func() { exec.Command("git", "config", "--global", "--unset", key).Run() }) //nolint:errcheck
	}
}

// TestInstallModalRepoEditInvalidatesCandidates 回归（缺陷：改了「仓库」输入框后
// 旧候选还在列表里，安装会打到新仓库）：候选只对发现它的那个仓库 + ref 有效。
// 步骤：用 A 发现候选 → 把仓库改成 B（不点安装）→ 旧候选必须失效、不再显示、
// 主按钮回到「安装」→ 再点安装应按 B 重新发现，绝不把 A 的路径拿去装 B。
func TestInstallModalRepoEditInvalidatesCandidates(t *testing.T) {
	repoA := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	repoB := seedRepo(t, t.TempDir(), "", "skills/gamma", "skills/delta")
	mirrorAliasPair(t, repoA, "owner/stale-a", repoB, "owner/stale-b")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	// 1-2. 用 owner/stale-a 发现候选，然后把仓库输入框改成 owner/stale-b。
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/stale-a"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 || !tt.HasText("skills/alpha") || !tt.HasText("skills/beta") {
		t.Fatalf("A 应列出 2 个候选：%v 文本 %v", a.install.cands, tt.Texts())
	}
	a.install.repo = "owner/stale-b"
	refresh(a, tt)

	// 3. 旧候选失效：不再有效、不再显示，主按钮回到「安装」。
	if a.install.candsLive() {
		t.Errorf("改了仓库后旧候选不应再有效：%v（发现于 %s@%s）", a.install.cands, a.install.candRepo, a.install.candRef)
	}
	if tt.HasText("skills/alpha") || tt.HasText("skills/beta") {
		t.Errorf("旧候选不应继续显示，文本 %v", tt.Texts())
	}
	if got := installButtonLabel(&a.install); got != "安装" {
		t.Errorf("旧候选失效后主按钮应回到「安装」，得到 %q", got)
	}
	if !tt.HasText("仓库或分支已改") {
		t.Errorf("应提示候选已失效，文本 %v", tt.Texts())
	}

	// 4. 最危险的一步：旧候选（或已被勾上的旧候选）还在时点安装。修复后按钮
	// 先清掉失效候选、按表单重新发现；无论如何都不能把 A 的路径送给 B。
	// 这里直接勾上内存里的旧候选（模拟「用户勾完才发现改错了仓库」），
	// 这样即使界面上已经不显示它们，也仍然在考验点击路径上的保护。
	for i := range a.install.cands {
		a.install.cands[i].on = true
	}
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if a.install.err != "" {
		t.Errorf("重新发现不应报错，得到 %q", a.install.err)
	}
	for _, stale := range []string{"skills/alpha", "skills/beta"} {
		if strings.Contains(a.install.err, stale) {
			t.Errorf("绝不允许把 A 的候选（%s）装到 B：err=%q", stale, a.install.err)
		}
	}
	for _, stale := range []string{"alpha", "beta"} {
		if _, err := os.Stat(cfg.SkillPath(stale)); err == nil {
			t.Errorf("A 的技能 %s 不应出现在根目录里", stale)
		}
	}
	if len(a.install.cands) != 2 || a.install.cands[0].path != "skills/delta" || a.install.cands[1].path != "skills/gamma" {
		t.Errorf("应重新发现 B 的候选，得到 %v", a.install.cands)
	}
	if entries, _ := os.ReadDir(cfg.Root); len(entries) != 0 {
		t.Errorf("这一步不该装上任何技能，得到 %v", entries)
	}
	// 勾 B 的一个候选装上：确认后续走的也是新仓库。
	click(t, a, tt, "skills/gamma")
	click(t, a, tt, "确认安装")
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("gamma"), "SKILL.md")); err != nil {
		t.Errorf("gamma 应装上: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("alpha")); err == nil {
		t.Error("不该出现 alpha（那是 A 的技能）")
	}
}

// TestInstallModalRefEditInvalidatesCandidates 回归：改「分支/标签」与改仓库同理
// （候选只对发现它的那个 ref 有效），并且改回原值时候选自然又有效（判据是纯
// 比较，没有把候选丢掉——避免输入抖动就是这个意思）。
func TestInstallModalRefEditInvalidatesCandidates(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	mirrorAlias(t, repo, "owner/refedit")

	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/refedit"
	refresh(a, tt)
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 {
		t.Fatalf("应列出 2 个候选，得到 %v", a.install.cands)
	}

	a.install.ref = "dev"
	refresh(a, tt)
	if a.install.candsLive() || tt.HasText("skills/alpha") {
		t.Errorf("改了分支后旧候选不应再有效或显示：%v 文本 %v", a.install.cands, tt.Texts())
	}
	if got := installButtonLabel(&a.install); got != "安装" {
		t.Errorf("旧候选失效后主按钮应回到「安装」，得到 %q", got)
	}

	// 改回原值：候选与勾选还在，自然又有效。
	a.install.ref = ""
	refresh(a, tt)
	if !a.install.candsLive() || !tt.HasText("skills/alpha") {
		t.Errorf("改回原值时候选应重新有效：live=%v 文本 %v", a.install.candsLive(), tt.Texts())
	}
}

// holdSpawn 把 a.spawn 换成「只记账不执行」，返回放行函数与调用次数计数：用来在
// 后台发现/安装任务真正跑之前检查中间状态（newTestApp 的 spawn 默认同步执行）。
// 放行时仍然计数，所以「有没有再发起后续任务」也能数出来。
func holdSpawn(a *App) (release func(), count func() int) {
	orig := a.spawn
	var held []func()
	n := 0
	a.spawn = func(fn func()) { n++; held = append(held, fn) }
	return func() {
		a.spawn = func(fn func()) { n++; orig(fn) }
		for _, fn := range held {
			fn()
		}
		held = nil
	}, func() int { return n }
}

// TestInstallModalInFlightRepoEditDiscardsDiscovery 回归（缺陷：发现还在跑的时候
// 改仓库，回来时单候选的自动安装会装到新仓库上）：挂起后台任务，用 A 发起发现，
// 改成 B 后放行——这次发现整份作废：不填候选、不写子目录、不自动安装、不报错，
// 下一次点安装按 B 重新发现。B 里故意放一个同名目录（skills/solo-a）：修复前会
// 「装成功」，也就是用户没点第二次安装却装上了 B 的内容。
func TestInstallModalInFlightRepoEditDiscardsDiscovery(t *testing.T) {
	repoA := seedRepo(t, t.TempDir(), "", "skills/solo-a")
	repoB := seedRepo(t, t.TempDir(), "", "skills/solo-a", "skills/gamma")
	mirrorAliasPair(t, repoA, "owner/inflight-a", repoB, "owner/inflight-b")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/inflight-a"
	refresh(a, tt)

	// 点「安装」只发起发现（后台任务挂着不跑）。
	release, spawned := holdSpawn(a)
	click(t, a, tt, "确认安装")
	if !a.install.busy {
		t.Fatal("点安装后应处于发现中的 busy 状态")
	}
	// 发现还没回来，用户把仓库改成 B。
	a.install.repo = "owner/inflight-b"
	refresh(a, tt)
	// 放行发现：A 的结果（单候选 skills/solo-a）回来了。
	release()
	refresh(a, tt)

	// ① 根目录里没有任何新技能（修复前这里会出现 B 的 solo-a）。
	if entries, _ := os.ReadDir(cfg.Root); len(entries) != 0 {
		t.Errorf("在途改仓库后不应装上任何技能，得到 %v", entries)
	}
	// ② 不写子目录、不填候选、不报错、不再忙碌。
	if a.install.subPath != "" {
		t.Errorf("不应把 A 的候选写进子目录，得到 %q", a.install.subPath)
	}
	if len(a.install.cands) != 0 || a.install.err != "" || a.install.busy {
		t.Errorf("这次发现应被整份丢弃：cands=%v err=%q busy=%v", a.install.cands, a.install.err, a.install.busy)
	}
	if a.statusErr {
		t.Errorf("丢弃在途结果不该报错：status=%q statusErr=%v", a.status, a.statusErr)
	}
	// ③ 没有发起后续安装任务。
	if n := spawned(); n != 1 {
		t.Errorf("不应发起后续安装任务：spawn 调用次数=%d", n)
	}
	// ④ 再点一次安装：按 B 重新发现（gamma、solo-a），装上 gamma。
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 || a.install.cands[0].path != "skills/gamma" || a.install.cands[1].path != "skills/solo-a" {
		t.Fatalf("应按 B 重新发现，得到 %v（err=%q）", a.install.cands, a.install.err)
	}
	click(t, a, tt, "skills/gamma")
	click(t, a, tt, "确认安装")
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("gamma"), "SKILL.md")); err != nil {
		t.Errorf("gamma 应装上: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("solo-a")); err == nil {
		t.Error("不该装上 solo-a（用户从没选过 B 里的那个技能）")
	}
}

// TestInstallModalInFlightRepoEditDropsMultiCandidates 同一条判据在多候选分支：
// 发现期间改仓库，多个候选也整份丢掉（不填候选、不显示列表、不报错）。
func TestInstallModalInFlightRepoEditDropsMultiCandidates(t *testing.T) {
	repoA := seedRepo(t, t.TempDir(), "", "skills/alpha", "skills/beta")
	repoB := seedRepo(t, t.TempDir(), "", "skills/gamma", "skills/delta")
	mirrorAliasPair(t, repoA, "owner/inflight-multi-a", repoB, "owner/inflight-multi-b")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/inflight-multi-a"
	refresh(a, tt)

	release, spawned := holdSpawn(a)
	click(t, a, tt, "确认安装")
	a.install.repo = "owner/inflight-multi-b"
	refresh(a, tt)
	release()
	refresh(a, tt)

	if len(a.install.cands) != 0 || a.install.err != "" || a.statusErr {
		t.Errorf("多候选结果也应整份丢弃：cands=%v err=%q status=%q statusErr=%v",
			a.install.cands, a.install.err, a.status, a.statusErr)
	}
	if tt.HasText("skills/alpha") || tt.HasText("skills/beta") {
		t.Errorf("旧仓库的候选不应显示，文本 %v", tt.Texts())
	}
	if entries, _ := os.ReadDir(cfg.Root); len(entries) != 0 {
		t.Errorf("不该装上任何技能，得到 %v", entries)
	}
	if n := spawned(); n != 1 {
		t.Errorf("不应发起安装任务：spawn 调用次数=%d", n)
	}
	// 下一次点安装按 B 重新发现。
	click(t, a, tt, "确认安装")
	if len(a.install.cands) != 2 || a.install.cands[0].path != "skills/delta" || a.install.cands[1].path != "skills/gamma" {
		t.Errorf("应按 B 重新发现，得到 %v（err=%q）", a.install.cands, a.install.err)
	}
}

// TestInstallModalInFlightSubPathEditSkipsAutoInstall 同一类问题：发现期间用户
// 自己填了「子目录」，唯一候选的自动安装不得覆盖他的输入、也不得自动装
// （「手填子目录就装那一个」这条既有行为）。
func TestInstallModalInFlightSubPathEditSkipsAutoInstall(t *testing.T) {
	repo := seedRepo(t, t.TempDir(), "", "skills/solo-a")
	mirrorAlias(t, repo, "owner/inflight-sub")

	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "安装技能")
	a.install.repo = "owner/inflight-sub"
	refresh(a, tt)

	release, spawned := holdSpawn(a)
	click(t, a, tt, "确认安装")
	// 发现还没回来，用户自己填了子目录。
	a.install.subPath = "skills/manual"
	refresh(a, tt)
	release()
	refresh(a, tt)

	if a.install.subPath != "skills/manual" {
		t.Errorf("不应覆盖用户填的子目录，得到 %q", a.install.subPath)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("solo-a"), "SKILL.md")); err == nil {
		t.Error("自动安装不应发生（用户没点第二次安装）")
	}
	if n := spawned(); n != 1 {
		t.Errorf("不应发起安装任务：spawn 调用次数=%d", n)
	}
	if a.statusErr || !a.install.open || a.install.busy {
		t.Errorf("应安静地停下等用户动作：statusErr=%v open=%v busy=%v", a.statusErr, a.install.open, a.install.busy)
	}
	if got := installButtonLabel(&a.install); got != "安装" {
		t.Errorf("手填子目录时主按钮应写「安装」，得到 %q", got)
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
//
// 注意 KURA_UI_SHOTS 是相对测试包目录（internal/ui）解析的（shots_test.go 的
// 既有约定），想让产物落到仓库根的 tools/ 下要写绝对路径：
//
//	KURA_UI_SHOTS=$PWD/tools/multiinstall-window go test -count=1 -run TestRenderInstallCandidatesShot ./internal/ui/
func TestRenderInstallCandidatesShot(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过安装弹窗截图")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	cands := candidatesFromPaths([]string{
		"skills/engineering/ask-matt", "skills/engineering/code-review",
		"skills/engineering/codebase-design", "skills/engineering/diagnosing-bugs",
		"skills/engineering/domain-modeling", "skills/engineering/grill-with-docs",
		"skills/engineering/implement", "skills/engineering/pr",
		"skills/engineering/tdd", "skills/productivity/grill-me",
		"skills/productivity/grilling", "skills/productivity/handoff",
	})
	// 勾两个：截图里能看到复选框的勾选态与主按钮的计数。
	cands[0].on, cands[8].on = true, true
	a.install = installState{
		open: true, source: installGitHub, repo: "mattpocock/skills",
		// candRepo/candRef 必须与 repo 一起填：渲染时按 candsLive() 判断候选是否
		// 有效，漏填会被当成「仓库已改」而只画出失效提示（上一轮就是这个用例
		// 默默退化了）。这里 repo 解析出来就是 mattpocock/skills、ref 为空。
		candRepo: "mattpocock/skills",
		cands:    cands,
	}
	tt := newTester(a, 1140, 760)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()
	// 编码前先断言这一帧真的是候选列表（总数、勾选计数、候选行），不是失效提示：
	// 以后 candRepo/candRef 再被漏填，这里会直接失败而不是安静地存下一张废图。
	for _, want := range []string{"共 12 个技能", "安装选中的 2 个", "skills/engineering/tdd"} {
		if !tt.HasText(want) {
			t.Fatalf("截图帧里应出现 %q（候选列表而不是失效提示），文本 %v", want, tt.Texts())
		}
	}
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
