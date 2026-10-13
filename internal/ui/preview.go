package ui

// 检查更新与更新评审面板（里程碑 4）。
//
// 交互流：
//  1. 详情面板「检查更新」或工具栏「全部检查」→ PreviewUpdate 在后台运行；
//  2. 单技能结果进入 a.review（Modal 打开，每行一个变更文件 + Apply/取消），
//     失败进状态行，无变化也在状态行提示；
//  3. 「全部检查」逐个技能串行预览，汇总进同一个 Modal（每个技能一段），
//     Apply 逐个应用有变化的技能。
//
// 线程约定沿用 app.go：App 字段只在 UI 线程读写；磁盘 I/O 走 spawn，
// 结果经 publish 回 UI 线程；busy 只标记变更操作，检查/预览不置 busy。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
)

// skillReviewEntry 是「全部检查」汇总里一个技能的结果。
type skillReviewEntry struct {
	skill   string               // 技能名
	preview *store.UpdatePreview // 预览成功时非 nil
	err     string               // 预览失败原因；成功为空串
}

// reviewState 是评审面板（Modal）的数据：一个技能的预览，或全部检查汇总。
type reviewState struct {
	open bool
	// cfg 是检查时所在的目录；应用更新只作用于它。
	cfg *store.Config
	// skill 是评审针对的技能名；全部检查时为空串。
	skill string
	// all 表示全部检查汇总模式。
	all bool
	// preview 是单技能预览（all=false 时有效）。
	preview *store.UpdatePreview
	// entries 是全部检查的每技能结果（all=true 时有效），按技能名排序。
	entries []skillReviewEntry
	// applyBusy 是 Apply/Update 正在后台执行，防止重复点击。
	applyBusy bool
}

// hasCheckTarget 报告当前是否可以发起一次检查（避免并发检查互相覆盖）。
func (a *App) hasCheckTarget() bool {
	return a.checkingAll || a.checking != "" || a.review.applyBusy
}

// CheckAsync 检查单个技能的更新（后台执行 PreviewUpdate）。完成后：
// 有变化 → 打开评审面板；无变化 → 状态行提示；失败 → 状态行报错。
func (a *App) CheckAsync(name string) {
	if a.hasCheckTarget() {
		return
	}
	a.checking = name
	a.status, a.statusErr = "正在检查更新 "+name+"…", false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		p, err := cfg.PreviewUpdate(name)
		a.publish(func() {
			a.checking = ""
			if gen != a.gen {
				return // 期间切换了目录，结果作废
			}
			if err != nil {
				a.status, a.statusErr = fmt.Sprintf("检查 %s 失败: %v", name, err), true
				return
			}
			// 有无变化都打开面板：面板内显式提示「与上游一致」，比只靠
			// 状态行更清楚（无变化时应用按钮禁用）。
			a.review = reviewState{open: true, cfg: cfg, skill: name, preview: p}
			a.updates[name] = p.HasChanges()
			a.status, a.statusErr = checkSummary(name, p), false
		})
	})
}

// CheckAllAsync 依次检查根目录里全部 enabled 技能（后台串行预览），
// 完成后打开汇总评审面板。disabled/trashed 技能没有可更新的上游语义
// （Update 只作用于根目录技能），不参与检查。
func (a *App) CheckAllAsync() {
	if a.hasCheckTarget() {
		return
	}
	a.checkingAll = true
	a.status, a.statusErr = "正在检查全部更新…", false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		// 只扫根目录；扫描失败等价于没有可检查的技能。
		skills, err := cfg.Scan()
		entries := make([]skillReviewEntry, 0, len(skills))
		var scanErr string
		if err != nil {
			scanErr = err.Error()
		}
		for _, s := range skills {
			if scanErr != "" {
				entries = append(entries, skillReviewEntry{skill: s.Name, err: "根目录扫描失败: " + scanErr})
				continue
			}
			p, perr := cfg.PreviewUpdate(s.Name)
			if perr != nil {
				entries = append(entries, skillReviewEntry{skill: s.Name, err: perr.Error()})
				continue
			}
			entries = append(entries, skillReviewEntry{skill: s.Name, preview: p})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].skill < entries[j].skill })
		a.publish(func() {
			a.checkingAll = false
			if gen != a.gen {
				return // 期间切换了目录，结果作废
			}
			a.review = reviewState{open: true, cfg: cfg, all: true, entries: entries}
			for _, e := range entries {
				a.updates[e.skill] = e.preview != nil && e.preview.HasChanges()
			}
			a.status, a.statusErr = fmt.Sprintf("已检查 %d 个技能", len(entries)), false
		})
	})
}

// ApplyAsync 应用评审面板里的结果：单技能 → Update(name)；全部检查 →
// 对每个有变化的技能依次 Update。都在后台执行，完成后重新扫描并关闭面板。
func (a *App) ApplyAsync() {
	r := &a.review
	if r.applyBusy {
		return
	}
	// 收集本次要更新的技能名（在 UI 线程里确定，后台只执行）。
	var names []string
	if r.all {
		for _, e := range r.entries {
			if e.preview != nil && e.preview.HasChanges() {
				names = append(names, e.skill)
			}
		}
	} else if r.preview != nil && r.preview.HasChanges() {
		names = []string{r.skill}
	}
	if len(names) == 0 {
		return
	}
	r.applyBusy = true
	cfg, gen := r.cfg, a.gen
	if cfg == nil {
		cfg = a.cfg
	}
	a.status, a.statusErr = "正在更新 "+strings.Join(names, "、")+"…", false
	a.spawn(func() {
		var firstErr error
		for _, name := range names {
			if _, err := cfg.Update(name); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			r.applyBusy = false
			r.open = false // 更新完成后关闭评审面板
			if gen == a.gen {
				a.applyScan(skills, scanErr)
				for _, n := range names {
					delete(a.updates, n)
				}
			}
			if firstErr != nil {
				a.status, a.statusErr = "更新失败: "+firstErr.Error(), true
				return
			}
			a.status, a.statusErr = fmt.Sprintf("已更新 %d 个技能（旧内容在快照中）", len(names)), false
		})
	})
}

// CloseReview 关闭评审面板（取消按钮）。
func (a *App) CloseReview() { a.review.open = false }

// checkSummary 生成单技能检查完成的状态行文案。
func checkSummary(name string, p *store.UpdatePreview) string {
	added, changed, removed := p.Counts()
	return fmt.Sprintf("%s: 新增 %d · 修改 %d · 删除 %d（共 %d 个文件）",
		name, added, changed, removed, len(p.Files))
}
