package ui

// 历史面板只保存应用数据。列表、预览、逐行差异及恢复都在后台运行，
// 每次回写同时检查目录代次、面板身份和请求序号，不让旧任务覆盖新选择。

import (
	"fmt"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

type snapshotHistoryState struct {
	open, loading, busy, confirm bool
	cfg                          *store.Config
	name                         string
	state                        store.State
	gen                          int
	seq                          uint64
	items                        []store.Snapshot
	selected                     int
	preview                      *store.SnapshotPreview
	err                          string
	file                         string
	fileLoading                  bool
	fileSeq                      uint64
	diff                         *store.SnapshotTextDiff
}

func (a *App) OpenSnapshots(s *store.Skill) {
	if s == nil || (s.State != store.StateEnabled && s.State != store.StateDisabled) || a.isBusy(s.Name) {
		return
	}
	a.snapshotSeq++
	h := &snapshotHistoryState{open: true, loading: true, cfg: a.cfg, name: s.Name, state: s.State, gen: a.gen, seq: a.snapshotSeq, selected: -1}
	a.history = h
	cfg, name := h.cfg, h.name
	a.spawn(func() {
		items, err := cfg.ListSnapshots(name)
		a.publish(func() {
			if a.history != h || h.gen != a.gen {
				return
			}
			h.loading = false
			if err != nil {
				h.err = err.Error()
				return
			}
			h.items = items
			for i, item := range items {
				if item.Err == nil {
					a.selectSnapshot(i)
					break
				}
			}
		})
	})
}

func (a *App) selectSnapshot(index int) {
	h := a.history
	if h == nil || h.gen != a.gen || h.busy || index < 0 || index >= len(h.items) {
		return
	}
	h.selected = index
	h.seq++
	h.fileSeq++
	h.preview = nil
	h.diff = nil
	h.file = ""
	h.err = ""
	h.confirm = false
	h.fileLoading = false
	h.loading = false
	if h.items[index].Err != nil {
		h.err = h.items[index].Err.Error()
		return
	}
	h.loading = true
	cfg, name, state, stamp, seq := h.cfg, h.name, h.state, h.items[index].Stamp, h.seq
	a.spawn(func() {
		preview, err := cfg.PreviewSnapshot(name, state, stamp)
		a.publish(func() {
			if a.history != h || h.gen != a.gen || h.seq != seq {
				return
			}
			h.loading = false
			if err != nil {
				h.err = err.Error()
				return
			}
			h.preview = preview
			if len(preview.Files) > 0 {
				a.openSnapshotFile(preview.Files[0].Path)
			}
		})
	})
}

func (a *App) openSnapshotFile(path string) {
	h := a.history
	if h == nil || h.preview == nil || h.gen != a.gen || h.busy {
		return
	}
	h.file, h.diff, h.fileLoading = path, nil, true
	h.fileSeq++
	cfg, preview, seq := h.cfg, h.preview, h.fileSeq
	a.spawn(func() {
		diff, err := cfg.SnapshotFileDiff(preview, path)
		a.publish(func() {
			if a.history != h || h.gen != a.gen || h.fileSeq != seq {
				return
			}
			h.fileLoading = false
			if err != nil {
				h.err = err.Error()
				return
			}
			h.diff = diff
		})
	})
}

func (a *App) RestoreSnapshotAsync() {
	h := a.history
	if h == nil || h.gen != a.gen || h.busy || h.preview == nil || !h.preview.HasChanges() || a.isBusy(h.name) || a.hasCheckTarget() {
		return
	}
	h.busy, h.confirm = true, false
	h.err = ""
	key := a.busyKey(h.name)
	a.busy[key] = true
	cfg, preview, gen := h.cfg, h.preview, h.gen
	a.status, a.statusErr = locales.UiSnapshotRestoring(), false
	a.spawn(func() {
		_, err := cfg.RestoreSnapshot(preview)
		skills, scanErr := cfg.ScanAll()
		a.publish(func() {
			delete(a.busy, key)
			h.busy = false
			if a.gen != gen {
				return
			}
			if err == nil {
				a.applyScan(skills, scanErr)
			}
			if a.history != h {
				return
			}
			if err != nil {
				h.err = err.Error()
				a.status, a.statusErr = err.Error(), true
				return
			}
			h.open = false
			if scanErr != nil {
				a.status, a.statusErr = scanErr.Error(), true
				return
			}
			a.status, a.statusErr = locales.UiSnapshotRestored(h.name), false
			delete(a.updates, h.name)
		})
	})
}

func (a *App) viewSnapshotsModal(c *myui.Context) {
	h := a.history
	if h == nil || h.gen != a.gen {
		return
	}
	t := c.Theme()
	_, height := c.Size()
	// Modal 上下各留 Space(5)，面板内部还各有 Space(5) padding。
	// 主体高度明确受可用空间约束，差异区伸缩，按钮行始终留在面板内部。
	bodyHeight := min(float32(610), height-t.Space(20))
	listHeight := min(float32(105), max(float32(48), (height-320)/4))
	filesHeight := min(float32(78), max(float32(36), (height-320)/5))
	myui.Modal(c, &h.open, func() {
		myui.Column(c).Width(610).Height(bodyHeight).MinHeight(0).Gap(t.Space(2)).Children(func() {
			myui.Text(c, locales.UiSnapshotTitle(h.name)).Bold().FontSize(t.FontSize + 2).Shrink(0)
			myui.Scroll(c.Key("snapshot-list")).Height(listHeight).MinHeight(0).FillWidth().Children(func() {
				myui.Column(c).FillWidth().Gap(4).Children(func() {
					if !h.loading && len(h.items) == 0 && h.err == "" {
						myui.Text(c, locales.UiSnapshotEmpty()).TextColor(t.TextMuted)
					}
					for i, item := range h.items {
						label := item.Stamp
						if !item.Time.IsZero() {
							label = item.Time.Format("2006-01-02 15:04:05") + " · " + item.Stamp
						}
						button := myui.Button(c.Key(item.Stamp), label).FillWidth().Disabled(h.busy)
						if i == h.selected {
							button.Background(t.SurfacePressed)
						}
						if button.Clicked() {
							a.selectSnapshot(i)
						}
					}
				})
			})
			if h.loading {
				myui.Text(c, locales.UiSnapshotLoading()).TextColor(t.TextMuted)
			}
			if h.err != "" {
				myui.Text(c, h.err).MaxLines(3).TextColor(t.Danger)
			}
			if h.preview != nil {
				if !h.preview.HasChanges() {
					myui.Text(c, locales.UiSnapshotNoChanges()).TextColor(t.TextMuted)
				}
				if h.preview.MetadataChanged {
					myui.Text(c, locales.UiSnapshotMetadataChanged()).FontSize(t.FontSize - 1).TextColor(t.Warning)
				}
				myui.Scroll(c.Key("snapshot-files")).Height(filesHeight).MinHeight(0).FillWidth().Children(func() {
					myui.Column(c).FillWidth().Gap(3).Children(func() {
						for _, f := range h.preview.Files {
							label := diffStatusText(f.Status) + "  " + f.Path
							if f.Added >= 0 && f.Removed >= 0 {
								label = fmt.Sprintf("%s  +%d/-%d", label, f.Added, f.Removed)
							}
							if myui.Button(c.Key(f.Path), label).FillWidth().Disabled(h.busy).Clicked() {
								a.openSnapshotFile(f.Path)
							}
						}
					})
				})
			} else if !h.loading && h.err == "" {
				myui.Text(c, locales.UiSnapshotChoose()).TextColor(t.TextMuted)
			}
			if h.file != "" {
				myui.Text(c, h.file).SingleLine().Bold()
			}
			myui.Scroll(c.Key("snapshot-diff")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				myui.Column(c).FillWidth().Gap(1).Selectable().Children(func() {
					if h.fileLoading {
						myui.Text(c, locales.UiSnapshotLoading()).TextColor(t.TextMuted)
					}
					if h.diff != nil {
						if h.diff.Binary {
							myui.Text(c, locales.UiSnapshotBinary()).TextColor(t.TextMuted)
						}
						for _, line := range h.diff.Lines {
							color := t.Text
							if line.Kind == "+" {
								color = t.Success
							} else if line.Kind == "-" {
								color = t.Danger
							}
							myui.Text(c, line.Kind+" "+line.Text).TextColor(color)
						}
						if h.diff.Truncated {
							myui.Text(c, locales.UiSnapshotDiffTruncated()).TextColor(t.TextMuted)
						}
					}
				})
			})
			myui.Row(c).FillWidth().Shrink(0).Gap(8).Justify(myui.End).Children(func() {
				if myui.Button(c, locales.UiCommonClose()).Clicked() {
					h.open = false
					h.confirm = false
				}
				label := locales.UiSnapshotRestore()
				if h.busy {
					label = locales.UiSnapshotRestoring()
				}
				if myui.PrimaryButton(c, label).Disabled(h.busy || h.loading || h.preview == nil || !h.preview.HasChanges() || h.err != "" || a.hasCheckTarget()).Clicked() {
					h.confirm = true
				}
			})
		})
	})
	if !h.open {
		h.confirm = false
	}
	if myui.AlertDialog(c, &h.confirm, locales.UiSnapshotConfirmTitle(), locales.UiSnapshotConfirm(h.name), locales.UiCommonCancel(), locales.UiSnapshotRestore()) == 1 {
		a.RestoreSnapshotAsync()
	}
}
