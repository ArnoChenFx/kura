package ui

import "testing"

func TestParseRepo(t *testing.T) {
	cases := []struct{ in, repo, ref, sub string }{
		{"mattpocock/skills", "mattpocock/skills", "", ""},
		{"https://github.com/mattpocock/skills.git", "mattpocock/skills", "", ""},
		{"github.com/me/r/tree/dev/skills/tdd/", "me/r", "dev", "skills/tdd"},
		{" https://github.com/me/r/blob/main/a/SKILL.md ", "me/r", "main", "a/SKILL.md"},
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
}
