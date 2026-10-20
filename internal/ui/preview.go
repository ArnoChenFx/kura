package ui

// 检查更新与更新评审面板（里程碑 4）。
//
// 交互流：
//  1. 详情面板「检查更新」或工具栏「全部检查」→ PreviewUpdate 在后台运行；
//  2. 单技能结果进入 a.review（Modal 打开，每行一个变更文件 + Apply/取消），
//     失败进状态行，无变化也在状态行提示；
//  3. 「全部检查」按仓库共用预览，汇总进同一个 Modal（每个技能一段），
//     Apply 批量应用有变化的技能。
//
// 线程约定沿用 app.go：App 字段只在 UI 线程读写；磁盘 I/O 走 spawn，
// 结果经 publish 回 UI 线程；busy 只标记变更操作，检查/预览不置 busy。

import (
	"context"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// skillReviewEntry 是「全部检查」汇总里一个技能的结果。
type skillReviewEntry struct {
	skill    string               // 技能名
	preview  *store.UpdatePreview // 预览成功时非 nil
	err      string               // 预览失败原因；成功为空串
	selected bool                 // 用户本次勾选；本地有修改的技能默认不勾
	applied  bool                 // 已成功更新，不能重复应用旧预览
	applyErr string               // 上次应用失败的原因；保留用于逐项重试
}

// canApply 只接受预览成功、有变化且尚未成功应用的条目。
func (e *skillReviewEntry) canApply() bool {
	return e.err == "" && !e.applied && e.preview != nil && e.preview.HasChanges()
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

// selectedUpdateCount 是当前勾选且可执行的更新数。
func (r *reviewState) selectedUpdateCount() int {
	count := 0
	for i := range r.entries {
		if e := &r.entries[i]; e.selected && e.canApply() {
			count++
		}
	}
	return count
}

// failedUpdateCount 是当前勾选且上次应用失败的更新数；取消勾选后不参与重试。
func (r *reviewState) failedUpdateCount() int {
	count := 0
	for i := range r.entries {
		if e := &r.entries[i]; e.selected && e.canApply() && e.applyErr != "" {
			count++
		}
	}
	return count
}

// hasCheckTarget 同时拦住当前目录的写入：恢复/移动尚未完成时不能用一份
// 中间状态生成新的评审；其他根目录仍在完成的写入不阻塞当前目录浏览。
func (a *App) hasCheckTarget() bool {
	if a.checkingAll || a.checking != "" || a.review.applyBusy {
		return true
	}
	prefix := a.cfg.Root + "\x00"
	for key, busy := range a.busy {
		if busy && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// cancelCheck 在切目录或主动取消时立即释放检查状态，旧回调由序号挡住。
// 这里只取消预览；已经开始的替换写入仍由快照与原子替换路径完整执行。
func (a *App) cancelCheck() {
	if a.checkCancel != nil {
		a.checkCancel()
		a.checkCancel = nil
	}
	a.checkSeq++
	a.checking, a.checkingAll = "", false
}

// CancelCheck 取消当前的技能更新检查，不关闭已完成的评审结果。
func (a *App) CancelCheck() {
	if a.checking == "" && !a.checkingAll {
		return
	}
	a.cancelCheck()
	a.status, a.statusErr = locales.UiStatusCheckCancelled(), false
}

func (a *App) beginCheck() (context.Context, uint64) {
	a.checkSeq++
	ctx, cancel := context.WithCancel(context.Background())
	a.checkCancel = cancel
	return ctx, a.checkSeq
}

// finishCheck 只能清理自己那次任务；旧任务不能清掉新检查的忙碌标记。
func (a *App) finishCheck(gen int, seq uint64) bool {
	if seq != a.checkSeq || gen != a.gen {
		return false
	}
	if a.checkCancel != nil {
		a.checkCancel()
		a.checkCancel = nil
	}
	a.checking, a.checkingAll = "", false
	return true
}

// CheckAsync 检查单个技能的更新（后台执行 PreviewUpdate）。完成后：
// 有变化 → 打开评审面板；无变化 → 状态行提示；失败 → 状态行报错。
func (a *App) CheckAsync(name string) {
	if a.hasCheckTarget() {
		return
	}
	a.checking = name
	ctx, seq := a.beginCheck()
	a.status, a.statusErr = locales.UiStatusCheckingUpdate(name), false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		p, err := cfg.PreviewUpdateContext(ctx, name)
		a.publish(func() {
			if !a.finishCheck(gen, seq) {
				return // 期间切换了目录，结果作废
			}
			if err != nil {
				a.status, a.statusErr = locales.UiStatusCheckFailed(name, err), true
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

// CheckAllAsync 检查根目录里全部 enabled 技能（后台按仓库批量预览），
// 完成后打开汇总评审面板。disabled/trashed 技能没有可更新的上游语义
// （Update 只作用于根目录技能），不参与检查。
func (a *App) CheckAllAsync() {
	if a.hasCheckTarget() {
		return
	}
	a.checkingAll = true
	ctx, seq := a.beginCheck()
	a.status, a.statusErr = locales.UiStatusCheckingAll(), false
	cfg, gen := a.cfg, a.gen
	a.spawn(func() {
		if ctx.Err() != nil {
			return
		}
		// 只扫根目录；扫描失败明确报告，不当作「没有更新」。
		skills, err := cfg.Scan()
		entries := make([]skillReviewEntry, 0, len(skills))
		if err == nil {
			names := make([]string, len(skills))
			for i, s := range skills {
				names[i] = s.Name
			}
			// 批量检查：同一仓库的技能共用一次 ls-remote / clone，上游没变的
			// 技能不下载任何文件。
			for _, r := range cfg.PreviewUpdatesContext(ctx, names) {
				if r.Err != nil {
					entries = append(entries, skillReviewEntry{skill: r.Name, err: r.Err.Error()})
					continue
				}
				entries = append(entries, skillReviewEntry{
					skill: r.Name, preview: r.Preview,
					selected: r.Preview.HasChanges() && !r.Preview.LocalModified,
				})
			}
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].skill < entries[j].skill })
		a.publish(func() {
			if !a.finishCheck(gen, seq) {
				return // 期间切换了目录，结果作废
			}
			if err != nil {
				a.status, a.statusErr = locales.UiReviewScanFailed(err.Error()), true
				return
			}
			a.review = reviewState{open: true, cfg: cfg, all: true, entries: entries}
			for _, e := range entries {
				a.updates[e.skill] = e.preview != nil && e.preview.HasChanges()
			}
			a.status, a.statusErr = locales.UiStatusCheckedAll(len(entries)), false
		})
	})
}

// ApplyAsync 只应用用户勾选的批量条目；单技能沿用原来的应用行为。
// 批量结束保留面板与逐项结果，成功条目不能再次执行旧预览。
func (a *App) ApplyAsync() { a.applyReviewAsync(false) }

// RetryFailedUpdates 只重试仍勾选的失败条目，不连带执行尚未尝试的新条目。
func (a *App) RetryFailedUpdates() {
	if a.review.all {
		a.applyReviewAsync(true)
	}
}

func (a *App) applyReviewAsync(failedOnly bool) {
	r := &a.review
	if r.applyBusy {
		return
	}
	// 收集本次要更新的技能名（在 UI 线程里确定，后台只执行）。
	var names []string
	if r.all {
		for i := range r.entries {
			e := &r.entries[i]
			if e.selected && e.canApply() && (!failedOnly || e.applyErr != "") {
				names = append(names, e.skill)
			}
		}
	} else if r.preview != nil && r.preview.HasChanges() {
		names = []string{r.skill}
	}
	if len(names) == 0 {
		return
	}
	cfg, gen := r.cfg, a.gen
	if cfg == nil {
		cfg = a.cfg
	}
	// 先核对整批，确认没有快照恢复/移动等写入后才一次性登记。不能只靠
	// 模态窗口挡操作：用户关闭窗口或切换目录后，后台写入仍会继续。
	keys := make([]string, len(names))
	for i, name := range names {
		key := cfg.Root + "\x00" + name
		if a.busy[key] {
			a.status, a.statusErr = locales.UiDetailBusy(name), false
			return
		}
		keys[i] = key
	}
	for _, key := range keys {
		a.busy[key] = true
	}
	r.applyBusy = true
	a.status, a.statusErr = locales.UiStatusUpdating(strings.Join(names, locales.UiCommonListSep())), false
	a.spawn(func() {
		// 批量更新：同一仓库的技能共用一次 clone。
		var done []string
		var firstErr error
		failed := 0
		results := cfg.UpdateMany(names)
		for _, res := range results {
			if res.Err != nil {
				failed++
				if firstErr == nil {
					firstErr = res.Err
				}
				continue
			}
			done = append(done, res.Name)
		}
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			// 不论用户是否切换了目录，都要释放这次任务登记的原目录写入。
			for _, key := range keys {
				delete(a.busy, key)
			}
			if gen != a.gen {
				return // 写入照常完成，旧评审不能关闭新目录的评审面板。
			}
			r.applyBusy = false
			if r.all {
				// 只更新这次实际执行的条目；取消勾选与尚未尝试的条目原样保留。
				byName := make(map[string]store.UpdateResult, len(results))
				for _, result := range results {
					byName[result.Name] = result
				}
				for i := range r.entries {
					e := &r.entries[i]
					result, attempted := byName[e.skill]
					if !attempted {
						continue
					}
					if result.Err != nil {
						e.applyErr = result.Err.Error()
					} else {
						e.applied, e.selected, e.applyErr = true, false, ""
					}
				}
			} else {
				r.open = false
			}
			a.applyScan(skills, scanErr)
			// 只清掉更新成功的；失败的仍然「有更新」。
			for _, n := range done {
				delete(a.updates, n)
			}
			switch {
			case firstErr == nil:
				a.status, a.statusErr = locales.UiStatusUpdated(len(done)), false
			case len(done) == 0:
				a.status, a.statusErr = locales.UiStatusUpdateFailed(firstErr.Error()), true
			default:
				a.status, a.statusErr = locales.UiStatusUpdatedPartial(len(done), failed, firstErr), true
			}
		})
	})
}

// CloseReview 关闭评审面板（取消按钮）。
func (a *App) CloseReview() { a.review.open = false }

// checkSummary 生成单技能检查完成的状态行文案。
func checkSummary(name string, p *store.UpdatePreview) string {
	added, changed, removed := p.Counts()
	return locales.UiStatusCheckSummary(name, added, changed, removed, len(p.Files))
}
