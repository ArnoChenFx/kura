package ui

import (
	"slices"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

func TestLocalSearchMatchesRepositoryLinksAndKeepsOtherFields(t *testing.T) {
	repo := &store.Skill{Name: "alpha", Description: "Review changes", State: store.StateEnabled,
		Meta: &store.Meta{DisplayName: "Friendly reviewer", Source: store.Source{Type: store.SourceGitHub, URL: "https://github.com/Owner/Toolbox.git"}}}
	bare := &store.Skill{Name: "beta", State: store.StateDisabled,
		Meta: &store.Meta{Source: store.Source{Type: store.SourceGitHub, URL: "Owner/Toolbox"}}}
	local := &store.Skill{Name: "gamma", State: store.StateEnabled,
		Meta: &store.Meta{Source: store.Source{Type: store.SourceLocal, Path: "/work/team/own-skills"}}}
	other := &store.Skill{Name: "delta", State: store.StateEnabled,
		Meta: &store.Meta{Source: store.Source{Type: store.SourceGitHub, URL: "https://github.com/Other/Toolbox"}}}
	missing := &store.Skill{Name: "no-meta", State: store.StateEnabled}
	a := &App{skills: []*store.Skill{local, bare, missing, other, repo}}
	for _, query := range []string{
		"owner/toolbox", " OWNER/TOOLBOX ", "https://github.com/Owner/Toolbox",
		"https://github.com/Owner/Toolbox.git/", "http://github.com/owner/toolbox?plain=1#readme",
		"git@github.com:Owner/Toolbox.git", "ssh://git@github.com/Owner/Toolbox.git",
		"https://github.com/owner/toolbox/tree/main/skills/reviewer",
	} {
		t.Run(query, func(t *testing.T) {
			a.query = query
			if got := a.visible(); !slices.Equal(got, []*store.Skill{repo, bare}) {
				t.Fatalf("仓库查询 %q = %v", query, skillNames(got))
			}
		})
	}
	for query, want := range map[string]*store.Skill{
		"review changes": repo, "friendly reviewer": repo, "alpha": repo,
		"/work/team/own-skills": local, "other/toolbox": other, "no-meta": missing,
	} {
		a.query = query
		if got := a.visible(); !slices.Equal(got, []*store.Skill{want}) {
			t.Fatalf("查询 %q = %v", query, skillNames(got))
		}
	}
	a.query, a.filter = "owner/toolbox", filterDisabled
	if got := a.visible(); !slices.Equal(got, []*store.Skill{bare}) {
		t.Fatalf("来源搜索不能绕过状态筛选：%v", skillNames(got))
	}
	a.filter = filterAll
	replaced := *repo
	meta := *repo.Meta
	meta.Source.URL = "https://github.com/New/Repository"
	replaced.Meta = &meta
	a.skills[4] = &replaced
	if got := a.visible(); !slices.Equal(got, []*store.Skill{bare}) {
		t.Fatalf("扫描快照更新后不能沿用旧来源索引：%v", skillNames(got))
	}
}
