---
name: mygo-maintenance
description: Maintain Go applications using github.com/egoist/mygo, including UI changes, crash fixes, and dependency upgrades. Apply MyGo-specific lifetime, identity, threading, and testing practices.
---

# MyGo app maintenance

Check the app's pinned MyGo version and existing conventions before changing APIs. Locate its source with `go list -m -f '{{.Dir}}' github.com/egoist/mygo`; read `ui/doc.go` and relevant implementation comments. Read `docs/ui/migration.md` only for major-version upgrades, including minor-version bumps while on `0.x` (for example, `0.2.x` to `0.3.x`, or `1.x` to `2.x`). Confirm API availability against the pinned source.

The native UI rules below apply where the app uses package `ui`; preserve a web app's frontend and typed Go/TypeScript bindings. These rules were checked against MyGo 0.6.0; recheck the pinned source on later upgrades. Read the release's upgrade notes as well as the migration guide: the source migration does not cover changed test input or platform behavior.

## UI lifetime and identity

- Views receive a stable `*ui.Context` for their window. `Children(func())` shares it and changes its parent scope during the callback. Keep Context out of app state and workers; capture `c.Services()` for persistent clipboard/URL callbacks and background redraws.
- Constructors return checked `ui.Element` values. Elements and custom parts expire before the next build pass, including another pass in the same frame. Store app data, widget state and `ui.Handle`, never an element or parts. Zero elements are safe and `Valid()` is a quiet check; stale operations panic in development/Tester and become empty results or no-ops in production. Fix the ownership rather than relying on validity guards or recovery.
- Bind persistent identity with `.Bind(&handle)`. `handle.Focused(c)` and `FocusWithin(c)` read committed focus independently of construction order. `handle.Focus(c)` requests focus and waits while hidden; `CancelFocus(c)` cancels it. Handles support several windows; select one with its Context, and keep widget state separate per window.
- Use `.FocusBind(&a.pane, files)` when app data should request focus. The field holds desired focus; `ui.FocusedValue(c, &a.pane)` reads actual focus while a hidden target waits. Use a comparable field, unique target values and its zero value for no focus.
- Use `handle.Bounds(c)` for committed geometry in input callbacks. `c.Resolve(handle)` returns only the current build's element. Do not capture an element in a later input callback to query its bounds.
- Give controls stable, unique identity before construction with `c.Key(key)`, including stateful widgets. Container `.Key(key)` must precede children and local state. Configure fluent lists before `.Rows`; use `.ItemKey` for stable item identity and `ui.ListRow.Selected()`/`ListFocused()` for row styling.
- Keys follow Go equality: a pointer keys the object it points to, not a string representation. Prefer stable model IDs; do not allocate a new pointer key on every build.

## Input and actions

- A control writes bound input before its constructor returns; a container takes input as its child builder returns. Read the new value immediately and commit derived checkbox/segmented values with `Changed`/`Submitted`, or use callbacks. Bind directly to persistent model fields when possible. Input follows the options of the frame the user acted on, including `Disabled` and `ReadOnly`, rather than options configured later in the current build.
- `OnClick`/`OnShortcut`/`OnChange`/`OnSubmit` run after construction and configuration, before rebuilding. Prefer deferred actions when changing a collection being built. `Clicked()` and shortcut queries remain supported for local interactions.
- Declare scoped commands with `handle.OnShortcut(c, mods, key, fn)`, before or after Bind. Only an enabled control present in the current build takes its command. Use `c.OnShortcut` for commands in the current parent scope.

## State, work, and rendering

- Views may rebuild several times for one event. Trigger external side effects through handled actions or explicit guarded jobs, not unconditional rendering code.
- Keep slow I/O and expensive computation off the UI thread. Publish results through `Window.Update`; it is asynchronous. `Invalidate` requests redraws without synchronizing model writes. Cancel obsolete jobs or reject stale results after source changes or window closure.
- Keep drawing callbacks limited to painting; they can run repeatedly. Prefer MyGo widgets/base widgets to preserve keyboard, focus, disabled, and accessibility behavior. Follow the app's theme and sizing conventions.
- kura uses MyGo 0.6's native wheel scrolling with its default step and timing; precise touchpad input moves immediately. Use `ListState.ScrollTo` for explicit search/board resets; the framework cancels the old wheel glide. Preserve virtual-list anchors, per-root browsing position and reduced-motion behavior. Do not add an app-owned wheel animation or input interception layer.
- Partial redraw, incremental layout and hidden-window suspension are automatic. Do not assume a frame is a timer for background work. Use kura's `loadingIndicator` helper (`plugins/loading.Ring`) for task indicators; it follows the theme's size/color and requires a localized task label. The plugin handles reduced motion, visibility and progress accessibility; do not wrap it in another animation timer.

## Platform and packaging

- A native-only app with no page or `ui.WebView` can set `linux.webview` to `false` in `mygo.json`. Inspect the generated Debian dependencies and install script; GTK is still required.
- Keep native window buttons unless custom buttons are intentional. `HideWindowButtons` and `Element.MaximizeButton` support custom chrome and Windows 11 snap layouts, but need native-platform verification.
- Native-window menu shortcuts now work on Windows and Linux; preserve standard Edit/Window roles. Keep kura's updater background policy: the plugin's new launch-time checks must not reintroduce an automatic update window.

## Verification

- Reproduce UI failures with `ui.NewTester`. Exercise extra frames while elements disappear and return, plus relevant focus, typing, and shortcut behavior.
- In 0.6, `Tester.Scroll` simulates precise touchpad input; use `Tester.Wheel` for discrete wheel/glide tests and `Advance` for intermediate and settled frames. Keep assertions for reversal, accumulation, reduced motion and programmatic jumps. `Click`, `ClickWith` and `RightClick` reject elements whose centers are outside the window; scroll the real target into view and check the returned error instead of bypassing hit testing.
- Run targeted regression tests, `go tool mygo vet .` for stored build values and goroutine captures, and `CGO_ENABLED=0 go test ./...`. Use race checks when changing shared state, and inspect rendered output or the native app when changing layout/platform behavior, respecting the user's verification preferences.
- For upgrades, preview with `go tool mygo migrate-ui .`, apply with `-write`, and review persistent identity/services, separately assigned keys and derived bound values manually. Verify affected API/lifecycle assumptions against the new pinned source and rerun relevant UI tests. Report remaining platform-specific verification limits.
