package cli

// add 自动发现仓库内技能的 CLI 测试：多候选时列出清单、退出码 1，
// --json 给出结构化 candidates；单候选直接装。仓库全部本地构造，
// insteadOf 重写指向本地 bare（与 checkTestRepo 同法），不联网。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// discoverRepo 在 base 下构造技能仓库并提交；dirs 是技能目录（slash 相对
// 路径）。返回仓库目录。
func discoverRepo(t *testing.T, base string, dirs ...string) string {
	t.Helper()
	repo := filepath.Join(base, "repo")
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
	gitCLI(t, repo, "init", "-q")
	gitCLI(t, repo, "config", "user.email", "t@t")
	gitCLI(t, repo, "config", "user.name", "t")
	gitCLI(t, repo, "add", ".")
	gitCLI(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// mirrorAlias 镜像 repo 为 bare，并把 https://github.com/<alias>.git 的
// clone 重写到本地（隔离的临时全局配置）。返回 restore。
func mirrorAlias(t *testing.T, repo, alias string) func() {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitCLI(t, "", "clone", "-q", "--bare", repo, bare)
	key := "url." + bare + ".insteadOf"
	gitCLI(t, "", "config", "--global", key, "https://github.com/"+alias+".git")
	return func() { exec.Command("git", "config", "--global", "--unset", key).Run() } //nolint:errcheck
}

// TestAddGitHubDiscoverMultiCLI 验证：add --github 多候选时退出码 1，
// 文本模式列出全部候选；--json 给出 {error, repo, candidates}；随后用
// --sub-path 指定其一可以装上。
func TestAddGitHubDiscoverMultiCLI(t *testing.T) {
	e := newEnv(t)
	repo := discoverRepo(t, t.TempDir(),
		"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me")
	t.Cleanup(mirrorAlias(t, repo, "mattpocock/multi"))

	// 文本模式：退出 1，stderr 列出 3 个候选。
	code := e.run("add", "--github", "mattpocock/multi")
	if code != ExitError {
		t.Fatalf("多候选应退出 1, got %d, stderr=%s", code, e.stderr.String())
	}
	for _, want := range []string{
		"mattpocock/multi", "3 个技能", "--sub-path",
		"skills/engineering/tdd", "skills/engineering/code-review", "skills/productivity/grill-me",
	} {
		if !contains(e.stderr.String(), want) {
			t.Errorf("stderr 应包含 %q:\n%s", want, e.stderr.String())
		}
	}
	if e.stdout.Len() != 0 {
		t.Errorf("多候选失败时 stdout 应为空，得到 %q", e.stdout.String())
	}

	// JSON 模式：candidates 数组可直接当 --sub-path 用。
	e.stderr.Reset()
	code = e.run("--json", "add", "--github", "mattpocock/multi")
	if code != ExitError {
		t.Fatalf("JSON 模式也应退出 1, got %d", code)
	}
	var cj struct {
		Error      string   `json:"error"`
		Repo       string   `json:"repo"`
		Candidates []string `json:"candidates"`
	}
	if err := json.Unmarshal(e.stderr.Bytes(), &cj); err != nil {
		t.Fatalf("stderr 应是合法 JSON: %v\n%s", err, e.stderr.String())
	}
	if cj.Repo != "mattpocock/multi" || len(cj.Candidates) != 3 || cj.Candidates[0] != "skills/engineering/code-review" {
		t.Errorf("candidatesJSON 形状不符: %+v", cj)
	}

	// 挑一个候选装上：退出 0。
	code = e.run("add", "--github", "mattpocock/multi", "--sub-path", "skills/productivity/grill-me")
	if code != ExitOK {
		t.Fatalf("--sub-path 指定后应退出 0, got %d, stderr=%s", code, e.stderr.String())
	}
	if !contains(e.stdout.String(), "added: grill-me") {
		t.Errorf("应装上 grill-me，stdout=%s", e.stdout.String())
	}
}

// TestAddGitHubDiscoverSingleCLI 验证：唯一候选（清单声明的一个技能）直接
// 安装，不需要 --sub-path；安装名取技能目录名。
func TestAddGitHubDiscoverSingleCLI(t *testing.T) {
	e := newEnv(t)
	repo := discoverRepo(t, t.TempDir(), "skills/solo")
	if err := os.MkdirAll(filepath.Join(repo, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"skills":["./skills/solo"]}`
	if err := os.WriteFile(filepath.Join(repo, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCLI(t, repo, "add", ".")
	gitCLI(t, repo, "commit", "-q", "--amend", "-m", "init+manifest")
	t.Cleanup(mirrorAlias(t, repo, "owner/solo-repo"))

	code := e.run("add", "--github", "owner/solo-repo")
	if code != ExitOK {
		t.Fatalf("唯一候选应直接安装, got %d, stderr=%s", code, e.stderr.String())
	}
	if !contains(e.stdout.String(), "added: solo") {
		t.Errorf("应装上 solo，stdout=%s", e.stdout.String())
	}
}

// contains 是 strings.Contains 的短别名（测试里大量使用）。
func contains(s, sub string) bool { return len(sub) == 0 || index(s, sub) >= 0 }

// index 返回 sub 在 s 中首次出现的下标，找不到为 -1。
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
