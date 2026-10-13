package store

// 上游（git 来源）的轻量访问：检查更新与安装/更新共用。
//
// 设计要点：
//   - 检查更新先用 `git ls-remote` 取 ref 当前指向的 commit（只传几百字节），
//     与元数据记录的 commit 相同就判定「已是最新」，不下载任何内容。
//   - commit 变了再做一次「只要树、不要文件」的浅 clone（--filter=blob:none
//     --no-checkout），取技能子目录的树哈希，与元数据记录的树哈希比较：
//     仓库别处变了而技能目录没变，仍判定最新。
//   - 真正需要内容时（预览差异、安装、更新）用稀疏检出只取技能子目录的
//     文件，仓库里与技能无关的大文件不下载；同一仓库的多个技能共用一次 clone。
//   - 判断全靠 git 对象哈希，与本地时钟、文件修改时间无关。
//   - 每条 git 命令都有超时，并关闭交互式凭据提示，网络卡住不会无限挂起。
//   - 服务器不支持部分 clone（--filter）时 git 会自动退化为完整浅 clone，
//     结果仍然正确，只是下载量变大。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// git 命令的整体超时。ls-remote 只交换 ref 列表；clone 只取树；检出按需取文件。
// 慢但在持续传输的大仓库靠 stallArgs 之外的整体上限兜底，所以上限放得比较宽。
var (
	lsRemoteTimeout = 30 * time.Second
	cloneTimeout    = 10 * time.Minute
	checkoutTimeout = 10 * time.Minute
)

// stallArgs 让 git 的 HTTP(S) 传输在速率低于 1 KB/s 且持续 30 秒时中止：
// 网络卡死很快失败，慢但还在传的大仓库不受影响。用 -c 传入，不写进仓库配置，
// 子进程（按需取对象的 fetch）会继承。
var stallArgs = []string{"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=30"}

// cloneHook 在每次 cloneUpstream 开始时被调用（只给测试统计 clone 次数）。
var cloneHook func(url string)

// ErrGitTimeout 表示 git 命令在限定时间内没有完成（多半是网络卡住）。
var ErrGitTimeout = errors.New("store: git 操作超时")

// gitOutput 在 dir 下执行 git（dir 为空时用当前目录），stdin 非空时作为标准
// 输入，返回原样的 stdout。失败时错误里带上 stderr；超时返回 ErrGitTimeout。
func gitOutput(timeout time.Duration, dir, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, stallArgs...), args...)...)
	cmd.Dir = dir
	// 没有凭据时直接失败，而不是等待一个永远不会有人回答的提示。
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%w（%s 超过 %s）", ErrGitTimeout, gitVerb(args), timeout)
	}
	if err != nil {
		return "", fmt.Errorf("store: git %s 失败: %w: %s", gitVerb(args), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// runGit 同 gitOutput，但不带标准输入，并去掉 stdout 的首尾空白。
func runGit(timeout time.Duration, dir string, args ...string) (string, error) {
	out, err := gitOutput(timeout, dir, "", args...)
	return strings.TrimSpace(out), err
}

// gitVerb 返回 git 子命令名（跳过 -C 等前置参数），用于错误信息。
func gitVerb(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" || args[i] == "-c" {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

// githubURL 把 owner/repo 换成 clone 地址。
func githubURL(repo string) string { return fmt.Sprintf("https://github.com/%s.git", repo) }

// repoKey 标识一个上游仓库的某个 ref：同一 key 的技能共用一次 clone。
// URL 只去掉末尾的 / 与 .git 后比较，所以 `.../r` 与 `.../r.git` 算同一个仓库。
type repoKey struct{ url, ref string }

func newRepoKey(url, ref string) repoKey {
	u := strings.TrimRight(url, "/")
	u = strings.TrimSuffix(u, ".git")
	return repoKey{u, ref}
}

// remoteHead 用 git ls-remote 取 ref 当前指向的 commit，不下载任何对象。
// ref 为空时取远端默认分支（HEAD）。ref 同时是分支和标签时与 git clone
// --branch 一致，分支优先；附注标签取剥离后的 commit。
func remoteHead(url, ref string) (string, error) {
	args := []string{"ls-remote", url}
	if ref == "" {
		args = append(args, "HEAD")
	} else {
		// 附注标签的剥离行是 refs/tags/<ref>^{}，要单独列出模式才会输出。
		args = append(args, ref, ref+"^{}")
	}
	out, err := runGit(lsRemoteTimeout, "", args...)
	if err != nil {
		return "", err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok {
			refs[name] = sha
		}
	}
	want := []string{"HEAD"}
	if ref != "" {
		want = []string{"refs/heads/" + ref, "refs/tags/" + ref + "^{}", "refs/tags/" + ref}
	}
	for _, name := range want {
		if sha := refs[name]; sha != "" {
			return sha, nil
		}
	}
	if ref == "" {
		return "", fmt.Errorf("store: 远端 %s 没有默认分支", url)
	}
	return "", fmt.Errorf("store: 远端 %s 不存在分支或标签 %s", url, ref)
}

// upstream 是一次「只取树」的浅 clone，按需稀疏检出技能子目录。
// 用完必须调用 Close 删除临时目录。
type upstream struct {
	dir    string // clone 所在临时目录（仓库根）
	Commit string // clone 到的 commit
}

// cloneUpstream 浅 clone url（可选 ref）到临时目录：只取 commit 与树，
// 不取文件内容、不检出工作区。失败时不留下临时目录。
func cloneUpstream(url, ref string) (*upstream, error) {
	if cloneHook != nil {
		cloneHook(url)
	}
	tmp, err := os.MkdirTemp("", "kura-upstream-")
	if err != nil {
		return nil, fmt.Errorf("store: 创建临时目录失败: %w", err)
	}
	args := []string{"clone", "-q", "--depth", "1", "--filter=blob:none", "--no-checkout"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, tmp)
	if _, err := runGit(cloneTimeout, "", args...); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	u := &upstream{dir: tmp}
	if u.Commit, err = runGit(lsRemoteTimeout, tmp, "rev-parse", "HEAD"); err != nil {
		u.Close()
		return nil, err
	}
	return u, nil
}

// Close 删除 clone 的临时目录。
func (u *upstream) Close() { os.RemoveAll(u.dir) }

// treeResult 是一个仓库内目录的查询结果：Tree 为树哈希，Err 非 nil 表示
// 该路径在上游不存在或不是目录。
type treeResult struct {
	Tree string
	Err  error
}

// Trees 一次取出 subPaths 里每个目录的 git 树哈希（空串表示仓库根）。
// 只读取树对象，不触发文件下载；路径按字面匹配（不展开通配符）。
// 返回值以规范化后的路径（cleanSubPath）为键。
func (u *upstream) Trees(subPaths []string) (map[string]treeResult, error) {
	res := map[string]treeResult{}
	var specs []string
	var keys []string
	for _, p := range subPaths {
		sub := cleanSubPath(p)
		if _, dup := res[sub]; dup {
			continue
		}
		if strings.ContainsAny(sub, "\n\r") {
			res[sub] = treeResult{Err: fmt.Errorf("store: 仓库内不存在目录 %s", p)}
			continue
		}
		res[sub] = treeResult{}
		keys = append(keys, sub)
		if sub == "" {
			specs = append(specs, "HEAD^{tree}")
		} else {
			specs = append(specs, "HEAD:"+sub)
		}
	}
	if len(specs) == 0 {
		return res, nil
	}
	// cat-file --batch-check 每行输入一个对象说明，输出「<sha> <类型> <大小>」，
	// 对象不存在时输出「<说明> missing」。树对象在部分 clone 里都在本地。
	out, err := gitOutput(lsRemoteTimeout, u.dir, strings.Join(specs, "\n")+"\n", "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, key := range keys {
		var f []string
		if i < len(lines) {
			f = strings.Fields(lines[i])
		}
		if len(f) == 3 && f[1] == "tree" {
			res[key] = treeResult{Tree: f[0]}
			continue
		}
		res[key] = treeResult{Err: fmt.Errorf("store: 仓库内不存在目录 %s", key)}
	}
	return res, nil
}

// Tree 取单个目录的树哈希，是 Trees 的单路径形式。
func (u *upstream) Tree(subPath string) (string, error) {
	res, err := u.Trees([]string{subPath})
	if err != nil {
		return "", err
	}
	r := res[cleanSubPath(subPath)]
	return r.Tree, r.Err
}

// Checkout 把 subPaths 对应的目录检出到工作区，其余路径不检出、文件内容
// 也不下载（稀疏检出 + 部分 clone 按需取对象）。任一 subPath 为空表示
// 技能就是仓库根，此时检出整个仓库。
func (u *upstream) Checkout(subPaths []string) error {
	full := false
	var patterns strings.Builder
	for _, p := range subPaths {
		sub := cleanSubPath(p)
		if sub == "" {
			full = true
			break
		}
		patterns.WriteString("/" + escapeSparse(sub) + "/\n")
	}
	if !full {
		info := filepath.Join(u.dir, ".git", "info")
		if err := os.MkdirAll(info, 0o755); err != nil {
			return fmt.Errorf("store: 准备稀疏检出失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(info, "sparse-checkout"), []byte(patterns.String()), 0o644); err != nil {
			return fmt.Errorf("store: 准备稀疏检出失败: %w", err)
		}
		if _, err := runGit(lsRemoteTimeout, u.dir, "config", "core.sparseCheckout", "true"); err != nil {
			return err
		}
	}
	_, err := runGit(checkoutTimeout, u.dir, "read-tree", "-mu", "HEAD")
	return err
}

// Dir 返回检出后 subPath 在本机的目录。
func (u *upstream) Dir(subPath string) string {
	sub := cleanSubPath(subPath)
	if sub == "" {
		return u.dir
	}
	return filepath.Join(u.dir, filepath.FromSlash(sub))
}

// cleanSubPath 把仓库内路径规范成 git 用的形式：/ 分隔、无首尾 /。
func cleanSubPath(p string) string {
	return strings.Trim(filepath.ToSlash(p), "/")
}

// escapeSparse 转义稀疏检出模式里的通配与特殊字符，让路径按字面匹配。
func escapeSparse(p string) string {
	var b strings.Builder
	for _, r := range p {
		if strings.ContainsRune(`\*?[!#`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
