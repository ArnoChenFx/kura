package store

// 技能 ModTime（「最近什么时候动过」）的测试。
//
// 取值口径：max(技能目录自身的 mtime、目录里 SKILL.md 的 mtime、目录里
// .kura-meta.json 的 mtime)；任一取不到就跳过；不递归整棵树；技能目录是链接
// （联接点/目录符号链接）时跟随链接取目标的 mtime。
//
// mtime 一律用 os.Chtimes 在真实文件系统上造出来，不伪造时钟；所有目录都在
// t.TempDir() 里，绝不触碰真实的 ~/.agents/skills 与 ~/.kura。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mtimeBase 是造 mtime 用的基准时刻，各用例在它上面加整小时，彼此可区分。
// 用整秒：NTFS 的 100ns 精度与 ext4 的 ns 精度都能精确还原，断言可以用 Equal。
// 它远早于「现在」，所以任何「误用了当前时间」的实现（例如对链接用 Lstat 拿到建链
// 那一刻）都会与它拉开差距。
var mtimeBase = time.Date(2024, 3, 5, 6, 7, 8, 0, time.UTC)

// at 返回基准时刻之后 hours 小时。
func at(hours int) time.Time { return mtimeBase.Add(time.Duration(hours) * time.Hour) }

// setMTime 把 path（文件或目录）的 mtime 设成基准之后 hours 小时，返回设下的时刻。
// 目录要最后设：往目录里增删条目会把它的 mtime 改回「现在」。
func setMTime(t *testing.T, path string, hours int) time.Time {
	t.Helper()
	tm := at(hours)
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatalf("设置 mtime 失败 %s: %v", path, err)
	}
	return tm
}

// scanByName 扫描 c 的全部技能（启用 + 停用 + 回收站），按「状态:名字」索引。
func scanByName(t *testing.T, c *Config) map[string]*Skill {
	t.Helper()
	all, err := c.ScanAll()
	if err != nil {
		t.Fatalf("ScanAll 失败: %v", err)
	}
	out := make(map[string]*Skill, len(all))
	for _, s := range all {
		out[string(s.State)+":"+s.Name] = s
	}
	return out
}

// TestSkillModTimeIsTheLatestOfDirSkillMdAndMeta 验证口径的核心：目录、SKILL.md、
// .kura-meta.json 三个 mtime 取最大。三种「只有一个最新」的情形各对应一种真实操作：
// 只增删了子条目 → 目录最新；只改了 SKILL.md 正文 → 文件最新；刚安装/更新过 → 元数据最新。
func TestSkillModTimeIsTheLatestOfDirSkillMdAndMeta(t *testing.T) {
	cases := []struct {
		name              string
		dirH, mdH, metaH  int // 三个路径的 mtime（相对基准的小时数）
		wantH             int
		wantWhichIsNewest string
	}{
		{"只有目录最新（只增删了子条目）", 30, 10, 20, 30, "dir"},
		{"只有 SKILL.md 最新（只改了正文）", 10, 30, 20, 30, "SKILL.md"},
		{"只有元数据最新（刚更新过）", 10, 20, 30, 30, "meta"},
		{"三者相同", 5, 5, 5, 5, "all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestConfig(t)
			writeSkill(t, c.Root, "alpha", nil)
			dir := c.SkillPath("alpha")
			meta := filepath.Join(dir, MetaFileName)
			if err := os.WriteFile(meta, []byte(`{"metaVersion":1}`), 0o644); err != nil {
				t.Fatal(err)
			}
			setMTime(t, filepath.Join(dir, "SKILL.md"), tc.mdH)
			setMTime(t, meta, tc.metaH)
			setMTime(t, dir, tc.dirH) // 目录最后设

			got := scanByName(t, c)["enabled:alpha"]
			if got == nil {
				t.Fatal("没有扫到 alpha")
			}
			if want := at(tc.wantH); !got.ModTime.Equal(want) {
				t.Errorf("最新的是 %s：ModTime = %v，想要 %v", tc.wantWhichIsNewest, got.ModTime.UTC(), want.UTC())
			}
		})
	}
}

// TestSkillModTimeSkipsWhatCannotBeRead 验证任一路径取不到就跳过它，而不是整个失败
// 或当成零值参与比较：没有 SKILL.md、没有元数据时只剩目录；目录本身不存在时为零值。
func TestSkillModTimeSkipsWhatCannotBeRead(t *testing.T) {
	c := newTestConfig(t)

	// 1) 既没有 SKILL.md 也没有元数据：只剩目录自己的 mtime。
	bare := filepath.Join(c.Root, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	wantBare := setMTime(t, bare, 7)

	// 2) 没有 SKILL.md、有元数据：目录与元数据取较大的那个（这里元数据更大）。
	metaOnly := filepath.Join(c.Root, "meta-only")
	if err := os.MkdirAll(metaOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	mf := filepath.Join(metaOnly, MetaFileName)
	if err := os.WriteFile(mf, []byte(`{"metaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantMeta := setMTime(t, mf, 50)
	setMTime(t, metaOnly, 40)

	// 3) 有 SKILL.md、没有元数据：目录与 SKILL.md 取较大的那个（这里目录更大）。
	mdOnly := filepath.Join(c.Root, "md-only")
	writeSkill(t, c.Root, "md-only", nil)
	setMTime(t, filepath.Join(mdOnly, "SKILL.md"), 60)
	wantMD := setMTime(t, mdOnly, 70)

	got := scanByName(t, c)
	for name, want := range map[string]time.Time{"bare": wantBare, "meta-only": wantMeta, "md-only": wantMD} {
		s := got["enabled:"+name]
		if s == nil {
			t.Fatalf("没有扫到 %s", name)
		}
		if !s.ModTime.Equal(want) {
			t.Errorf("%s: ModTime = %v，想要 %v", name, s.ModTime.UTC(), want.UTC())
		}
	}

	// 目录本身都不存在：三个路径全取不到，返回零值而不是 panic 或「现在」。
	if z := skillModTime(filepath.Join(c.Root, "ghost")); !z.IsZero() {
		t.Errorf("不存在的目录应得到零值，得到 %v", z)
	}
}

// TestSkillModTimeDoesNotRecurse 钉住「别改成递归整棵树」：子目录里的文件、子目录里
// 同名的 SKILL.md、中间目录的 mtime 即使远在将来，也不影响 ModTime。assets 里的改动
// 不该让卡片顺序跳动，且递归会让大 assets 的技能扫描变慢。
func TestSkillModTimeDoesNotRecurse(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "heavy", map[string]string{
		"assets/model.bin":       "x",
		"assets/deep/notes.md":   "y",
		"nested/SKILL.md":        "z",
		"nested/.kura-meta.json": `{"metaVersion":1}`,
	})
	dir := c.SkillPath("heavy")
	// 子树里所有东西都远在将来（+1000 小时），自下而上设。
	for _, rel := range []string{
		"assets/deep/notes.md", "assets/deep", "assets/model.bin", "assets",
		"nested/SKILL.md", "nested/.kura-meta.json", "nested",
	} {
		setMTime(t, filepath.Join(dir, filepath.FromSlash(rel)), 1000)
	}
	// 三个被看的路径：SKILL.md 最大（+10），目录与元数据（不存在）较小或取不到。
	want := setMTime(t, filepath.Join(dir, "SKILL.md"), 10)
	setMTime(t, dir, 5)

	got := scanByName(t, c)["enabled:heavy"]
	if got == nil {
		t.Fatal("没有扫到 heavy")
	}
	if !got.ModTime.Equal(want) {
		t.Errorf("子树里的 mtime 不应参与：ModTime = %v，想要 %v（SKILL.md 的）", got.ModTime.UTC(), want.UTC())
	}
}

// TestSkillModTimeFollowsLinks 验证技能目录是链接（联接点 / 目录符号链接）时取目标的
// mtime，而不是链接本身的：链接是测试刚建的，自己的 mtime 是「现在」，远晚于目标被
// 设下的 2024 年；若实现误用 os.Lstat，ModTime 会变成「现在」，这里就会失败。
func TestSkillModTimeFollowsLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		outside := t.TempDir()
		writeSkill(t, outside, "linked", nil)
		real := filepath.Join(outside, "linked")
		meta := filepath.Join(real, MetaFileName)
		if err := os.WriteFile(meta, []byte(`{"metaVersion":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		setMTime(t, filepath.Join(real, "SKILL.md"), 11)
		want := setMTime(t, meta, 40) // 目标里最大的是元数据
		setMTime(t, real, 20)

		if err := os.MkdirAll(c.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		mk(filepath.Join(c.Root, "linked"), real)

		got := scanByName(t, c)["enabled:linked"]
		if got == nil {
			t.Fatal("没有扫到链接形式的技能 linked")
		}
		if !got.ModTime.Equal(want) {
			t.Errorf("应取链接目标里最大的 mtime：ModTime = %v，想要 %v", got.ModTime.UTC(), want.UTC())
		}
		// 目标自己的目录 mtime 也走 Stat：把元数据拿掉后，剩目标目录 20 与 SKILL.md 11 取大。
		if err := os.Remove(meta); err != nil {
			t.Fatal(err)
		}
		setMTime(t, real, 20) // 删文件改了目录 mtime，设回去
		got = scanByName(t, c)["enabled:linked"]
		if want := at(20); !got.ModTime.Equal(want) {
			t.Errorf("去掉元数据后应取目标目录的 mtime：ModTime = %v，想要 %v", got.ModTime.UTC(), want.UTC())
		}
	})
}

// TestScanAllFillsModTimeForEveryState 验证三种存放状态的技能都有 ModTime，并钉住
// 两个用户看得见的性质：
//   - 启用/停用只是移动目录，不改它自己的 mtime，所以拨一下开关不会让卡片跳到最前面；
//   - 回收站里的条目是 trash/<name> 容器，取到的是它被移入回收站的时间（刚发生），
//     不是技能当年的 mtime。
func TestScanAllFillsModTimeForEveryState(t *testing.T) {
	c := newTestConfig(t)
	for _, name := range []string{"stays", "toggled", "trashed"} {
		writeSkill(t, c.Root, name, nil)
		dir := c.SkillPath(name)
		setMTime(t, filepath.Join(dir, "SKILL.md"), 3)
		setMTime(t, dir, 3)
	}
	if _, err := c.Disable("toggled"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.Remove("trashed"); err != nil {
		t.Fatal(err)
	}

	got := scanByName(t, c)
	for _, key := range []string{"enabled:stays", "disabled:toggled", "trashed:trashed"} {
		if s := got[key]; s == nil || s.ModTime.IsZero() {
			t.Fatalf("%s 应有 ModTime，得到 %+v", key, s)
		}
	}
	if s := got["enabled:stays"]; !s.ModTime.Equal(at(3)) {
		t.Errorf("没动过的启用技能 ModTime = %v，想要 %v", s.ModTime.UTC(), at(3).UTC())
	}
	if s := got["disabled:toggled"]; !s.ModTime.Equal(at(3)) {
		t.Errorf("停用只是移动目录，不该改变 ModTime：得到 %v，想要 %v", s.ModTime.UTC(), at(3).UTC())
	}
	// 再启用回来，依然不变。
	if _, err := c.Enable("toggled"); err != nil {
		t.Fatal(err)
	}
	if s := scanByName(t, c)["enabled:toggled"]; !s.ModTime.Equal(at(3)) {
		t.Errorf("启用回来后 ModTime = %v，想要 %v", s.ModTime.UTC(), at(3).UTC())
	}
	if s := got["trashed:trashed"]; s.ModTime.Before(start.Add(-2 * time.Second)) {
		t.Errorf("回收站条目应取移入回收站的时间（约 %v），得到 %v", start.UTC(), s.ModTime.UTC())
	}
}
