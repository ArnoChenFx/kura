package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// benchmarkSkills 混合大小写、描述和状态，避免只测已经排好序的小写名称。
func benchmarkSkills(n int) []*store.Skill {
	skills := make([]*store.Skill, n)
	for i := range skills {
		state := store.StateEnabled
		if i%3 == 0 {
			state = store.StateDisabled
		}
		skills[i] = &store.Skill{
			Name:        fmt.Sprintf("Skill-%05d", n-i),
			Description: fmt.Sprintf("Description for Workflow %d with Markdown support", i%7),
			State:       state,
			Meta:        &store.Meta{DisplayName: fmt.Sprintf("Friendly Skill %d", i)},
		}
	}
	return skills
}

func BenchmarkVisible(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			b.Run("steady", func(b *testing.B) {
				a := &App{skills: benchmarkSkills(n), query: "workflow"}
				a.visible()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					a.visible()
				}
			})
			b.Run("query-change", func(b *testing.B) {
				a := &App{skills: benchmarkSkills(n)}
				queries := []string{"workflow", "skill", "friendly", "markdown"}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					a.query = queries[i%len(queries)]
					a.visible()
				}
			})
		})
	}
}

// TestVisibleTracksInputChanges 保留搜索、筛选和稳定排序语义；测试直接赋值
// skills 或复用切片换元素也必须刷新，不能只依赖生产扫描路径中的手工失效。
func TestVisibleTracksInputChanges(t *testing.T) {
	alpha := &store.Skill{Name: "Alpha", Description: "First description", State: store.StateEnabled}
	lower := &store.Skill{Name: "alpha", State: store.StateDisabled}
	beta := &store.Skill{Name: "beta", State: store.StateEnabled, Meta: &store.Meta{DisplayName: "Friendly display"}}
	trash := &store.Skill{Name: "archived", State: store.StateTrashed}
	a := &App{skills: []*store.Skill{beta, lower, trash, alpha}}
	assertRows := func(want ...*store.Skill) {
		t.Helper()
		if got := a.visible(); !slices.Equal(got, want) {
			names := make([]string, len(got))
			for i, s := range got {
				names[i] = string(s.State) + ":" + s.Name
			}
			t.Fatalf("visible = %v, want %v", names, want)
		}
	}
	assertRows(alpha, lower, beta)
	assertRows(alpha, lower, beta)
	if a.skills[0] != beta {
		t.Fatal("排序不能改写扫描原序列")
	}
	a.query = " FIRST "
	assertRows(alpha)
	a.query = "Friendly"
	assertRows(beta)
	a.query = ""
	a.filter = filterDisabled
	assertRows(lower)
	a.filter = filterTrashed
	assertRows(trash)
	a.filter = filterAll
	a.skills = []*store.Skill{beta, alpha}
	assertRows(alpha, beta)
	// 同一底层数组的元素替换也会使缓存失效。
	a.skills[0] = lower
	assertRows(alpha, lower)
	a.skills = append(a.skills, beta)
	assertRows(alpha, lower, beta)
	a.skills = nil
	assertRows()
}
