# Independent acceptance checks

These checks belong to the independent verifier. They do not replace the builders' tests and do not modify implementation files. All browser automation uses a new task-owned Chromium process and synthetic Linux fixtures. No user tabs, native Mac input, secrets, or model APIs are used.

## Reproduce

From the repository root:

```sh
npm ci --prefix qa
node qa/source-review.mjs
node qa/contract.mjs novnc
node qa/contract.mjs neko
node qa/workbench.mjs
node qa/runtime.mjs
```

`contract.mjs` makes read-only requests unless `--active` is present. Active checks change control ownership, so obtain a test window from the backend owner first. They do not run shell commands. Use `node qa/contract.mjs novnc --active` only during the reserved window.

During the reserved window, `node qa/desktop.mjs novnc basic` exercises the actual viewer and the independent normalized input path. `node qa/desktop.mjs novnc measure` collects 30 same-clock input-to-decoded-frame observations. Neko accepts the same commands; add `--native` for its official SDK data-channel diagnostic. The gated Neko viewer is a separate route. `observe` is a read-only decoded-stream check. All commands require scoped executor escalation in this Mac's sandbox for loopback/browser/Docker access; an EPERM result without that access is not a backend failure.

Evidence is written under `artifacts/independent-qa/`. An absent service is **UNTESTED**, never a finished verification or a working-desktop failure. Results distinguish API checks, actual decoded viewer input, direct X11 adapter input, screenshot capture, and restart.

## Required acceptance evidence

- Actual decoded noVNC canvas / Neko video changing after browser-viewer input, with screenshots and fixture event evidence.
- Viewer and normalized adapter paths each exercise text, modifiers, click, double click, context menu, drag/release, scroll and scaled corner mapping.
- Unleased and stale inputs rejected; human takeover blocks agent writes at enforcement; simultaneous requests and queued work are checked; takeover/blur/disconnect release held inputs.
- Fresh unique marker in screenshot capture; image dimensions, bytes, HTTP duration, capture source and coordinate scale documented.
- At least 30 sequential synthetic viewer input-to-decoded-frame samples per backend where practical, same monotonic browser clock and workload; median/p95/max, failures and sampling interval. HTTP screenshot duration is separate.
- Repeated task-owned startup; Linux wallpaper, panel, launcher, browser and terminal verified; persistence checked after coordinated restart.
- Actual loopback host port bindings, no privileged execution/broad mounts/host shell API, browser Origin/Host rejection, payload bounds and direct-transport ownership checked.
- Physical Mac trackpad pinch/swipe and physical audio listening remain **UNTESTED** unless personally performed. Wheel/drag/Control-wheel are **EMULATED** gestures.

Pass/fail/untested/emulated matrix and exact commands are maintained in `docs/verification.md`; live coordination is in `docs/status/qa.md`.

Statistics use the standard median (average middle pair for even n) and nearest-rank p95. Original final measurements are preserved unchanged; see artifacts/independent-qa/statistics-correction.json for the corrected98.05ms/62.05ms medians and provenance. No measurement rerun occurred.
