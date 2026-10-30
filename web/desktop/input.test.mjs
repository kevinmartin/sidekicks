import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import {
  DesktopInput,
  normalizedKey,
  mapPoint,
  streamGeometry,
} from "./input.mjs";
const tick = () => new Promise((resolve) => setImmediate(resolve));
function queueFixture() {
  let current = 1;
  const calls = [],
    failures = [],
    pending = [];
  const input = Object.create(DesktopInput.prototype);
  Object.assign(input, {
    active: 1,
    generation: 0,
    running: false,
    events: [],
    pressed: new Map(),
    mods: new Map(),
    buttons: new Set(),
    getEpoch: () => current,
    sync() {},
    box: { focus() {} },
    onFailure(error, epoch) {
      failures.push({ error, epoch });
    },
    send(events, epoch, signal) {
      calls.push({ events, epoch, signal });
      return new Promise((resolve, reject) =>
        pending.push({ resolve, reject }),
      );
    },
  });
  return {
    input,
    calls,
    failures,
    pending,
    setEpoch(value) {
      current = value;
    },
  };
}
test("physical key mapping preserves modifiers and US punctuation", () => {
  for (const [code, key, expected] of [
    ["KeyA", "A", "a"],
    ["Digit1", "!", "1"],
    ["Semicolon", ":", ";"],
    ["Slash", "?", "/"],
    ["Equal", "+", "="],
    ["ShiftLeft", "Shift", "Shift"],
    ["KeyL", "l", "l"],
    ["NumpadEnter", "Enter", "Enter"],
  ])
    assert.equal(normalizedKey({ code, key }), expected);
  assert.equal(normalizedKey({ code: "NumpadMultiply", key: "*" }), null);
  assert.equal(normalizedKey({ code: "Unknown", key: "Dead" }), null);
});
test("pointer mapping excludes letterbox borders and tolerates CSS fractions", () => {
  const rect = { left: 100, top: 50, width: 1440, height: 810 };
  assert.deepEqual(mapPoint(rect, 1280, 720, 437.5, 331.25), {
    x: 300,
    y: 250,
  });
  assert.deepEqual(mapPoint(rect, 1280, 720, -5, 3000), { x: 0, y: 719 });
  assert.equal(mapPoint({ ...rect, width: 0 }, 1280, 720, 1, 1), null);
  const element = {
    width: 1280,
    height: 720,
    getBoundingClientRect: () => ({
      left: 0,
      top: 0,
      width: 1440,
      height: 900,
    }),
  };
  assert.deepEqual(streamGeometry(element).rect, {
    left: 0,
    top: 45,
    width: 1440,
    height: 810,
  });
});
test("suspend drops queued stale input without aborting a live checked write", async () => {
  const f = queueFixture();
  f.input.enqueue({ type: "key", key: "Shift", action: "down" });
  f.input.enqueue({ type: "key", key: "a", action: "down" });
  assert.equal(f.calls.length, 1);
  assert.equal(f.calls[0].signal, undefined);
  f.input.suspend();
  assert.equal(f.input.events.length, 0);
  f.setEpoch(2);
  f.input.activate(2);
  f.input.enqueue({ type: "key", key: "b", action: "down" });
  assert.equal(f.calls.length, 1, "new write overlapped the old checked write");
  f.pending[0].resolve();
  await tick();
  assert.equal(f.calls.length, 2);
  assert.equal(f.calls[1].epoch, 2);
  assert.equal(f.calls[1].events[0].key, "b");
  assert.ok(
    !f.calls.some((call) => call.events.some((event) => event.key === "a")),
  );
  f.pending[1].resolve();
  await tick();
  assert.equal(f.failures.length, 0);
});
test("delayed old write failure cannot revoke a replacement epoch", async () => {
  const f = queueFixture();
  f.input.enqueue({ type: "key", key: "a", action: "down" });
  f.input.suspend();
  f.setEpoch(2);
  f.input.activate(2);
  f.input.enqueue({ type: "key", key: "b", action: "down" });
  f.pending[0].reject(Error("old request rejected409"));
  await tick();
  assert.equal(f.failures.length, 0);
  assert.equal(f.input.active, 2);
  assert.equal(f.calls[1].epoch, 2);
  f.pending[1].resolve();
  await tick();
});
test("queue remains bounded and coalesces only adjacent moves", () => {
  const f = queueFixture();
  f.input.running = true;
  f.input.enqueue({ type: "pointer", action: "move", x: 1, y: 1 });
  f.input.enqueue({ type: "pointer", action: "move", x: 2, y: 2 });
  f.input.enqueue({ type: "key", key: "Shift", action: "down" });
  f.input.enqueue({ type: "pointer", action: "move", x: 3, y: 3 });
  assert.equal(f.input.events.length, 3);
  assert.equal(f.input.events[0].event.x, 2);
  for (let i = 0; i < 300; i++)
    f.input.enqueue({ type: "key", key: "a", action: "down" });
  assert.equal(f.failures.length, 1);
  assert.equal(f.input.active, 0);
  assert.equal(f.input.events.length, 0);
});
test("releaseAll preserves write ordering and clears local held bookkeeping", async () => {
  const f = queueFixture();
  f.input.mods.set("Shift", true);
  f.input.buttons.add("left");
  f.input.enqueue({ type: "key", key: "Shift", action: "down" });
  f.input.releaseAll();
  assert.equal(f.input.mods.size, 0);
  assert.equal(f.input.buttons.size, 0);
  assert.equal(f.calls.length, 1);
  f.pending[0].resolve();
  await tick();
  assert.equal(f.calls[1].events[0].type, "releaseAll");
  f.pending[1].resolve();
  await tick();
});
function viewerFixture({ connectOnStart = true } = {}) {
  const elements = new Map(),
    listeners = new Map(),
    requests = [],
    rfbs = [],
    fullscreen = { entered: 0, exited: 0 };
  let responseEpoch = 1,
    autoConnect = connectOnStart,
    deferredRelease,
    deferredManualRelease,
    deferredAcquire,
    connectionTimer,
    releaseTimer;
  const element = (id) => {
    if (!elements.has(id))
      elements.set(id, {
        style: {},
        dataset: {},
        textContent: "",
        replaceChildren() {},
        querySelector() {
          return null;
        },
        getBoundingClientRect: () => ({ width: 1280, height: 720 }),
        requestFullscreen: async () => {
          fullscreen.entered++;
          context.document.fullscreenElement = elements.get(id);
        },
      });
    return elements.get(id);
  };
  class RFB {
    constructor() {
      this.handlers = new Map();
      this._rfbConnectionState = "connected";
      rfbs.push(this);
    }
    addEventListener(type, fn) {
      this.handlers.set(type, fn);
      if (type === "connect" && autoConnect)
        queueMicrotask(() => this.emit("connect"));
    }
    emit(type) {
      this.handlers.get(type)?.();
    }
    disconnect() {
      this.disconnectCalls = (this.disconnectCalls || 0) + 1;
      this.emit("disconnect");
    }
  }
  class Input {
    activate(epoch) {
      this.active = epoch;
    }
    suspend() {
      this.active = 0;
    }
    sync() {}
    destroy() {}
  }
  const response = (status, body) => ({
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  });
  const context = {
    RFB,
    DesktopInput: Input,
    streamGeometry() {},
    URLSearchParams,
    Promise,
    AbortController,
    Number,
    Error,
    setTimeout(fn, ms) {
      if (ms === 15000) return (connectionTimer = { fn, active: true });
      if (ms === 5000) return (releaseTimer = { fn, active: true });
      return setTimeout(fn, ms);
    },
    clearTimeout(id) {
      if (id && typeof id === "object" && "active" in id) id.active = false;
      else clearTimeout(id);
    },
    setInterval() {},
    location: {
      protocol: "http:",
      host: "127.0.0.1:1234",
      pathname: "/",
      hash: "",
    },
    history: { replaceState() {} },
    document: {
      getElementById: element,
      fullscreenElement: null,
      exitFullscreen: async () => {
        fullscreen.exited++;
        context.document.fullscreenElement = null;
      },
    },
    window: {
      addEventListener(type, fn) {
        listeners.set(type, fn);
      },
    },
    fetch: async (path, options = {}) => {
      const body = options.body ? JSON.parse(options.body) : null;
      requests.push({
        path,
        body,
        headers: options.headers,
        keepalive: options.keepalive,
        signal: options.signal,
      });
      if (body?.action === "release" && options.keepalive && deferredRelease)
        return new Promise((resolve) => {
          deferredRelease.resolve = resolve;
        });
      if (
        body?.action === "release" &&
        !options.keepalive &&
        deferredManualRelease
      )
        return new Promise((resolve, reject) => {
          deferredManualRelease.resolve = resolve;
          deferredManualRelease.reject = reject;
          options.signal?.addEventListener(
            "abort",
            () => reject(Error("release aborted")),
            { once: true },
          );
        });
      if (body?.action === "acquire" && deferredAcquire)
        return new Promise((resolve, reject) => {
          deferredAcquire.resolve = resolve;
          deferredAcquire.reject = reject;
          deferredAcquire.signal = options.signal;
        });
      return response(200, { ok: true, owned: true, epoch: responseEpoch });
    },
  };
  vm.createContext(context);
  const source = fs
    .readFileSync(new URL("./novnc/viewer.mjs", import.meta.url), "utf8")
    .replace(/^import .*;\n/gm, "");
  vm.runInContext(source, context);
  return {
    context,
    requests,
    rfbs,
    listeners,
    fullscreen,
    response,
    setEpoch(value) {
      responseEpoch = value;
    },
    setAutoConnect(value) {
      autoConnect = value;
    },
    deferRelease() {
      deferredRelease = {};
      return deferredRelease;
    },
    deferAcquire() {
      deferredAcquire = {};
      return deferredAcquire;
    },
    deferManualRelease() {
      deferredManualRelease = {};
      return deferredManualRelease;
    },
    clearDeferredAcquire() {
      deferredAcquire = undefined;
    },
    expireConnection() {
      assert.equal(connectionTimer?.active, true);
      connectionTimer.fn();
    },
    expireRelease() {
      assert.equal(releaseTimer?.active, true);
      releaseTimer.fn();
    },
  };
}
test("noVNC repeated Take retains the receive-only RFB object and epoch", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  const original = api.rfb;
  await api.take();
  await api.take();
  assert.equal(api.rfb, original);
  assert.equal(api.epoch, 1);
  assert.equal(api.rfb.viewOnly, true);
  assert.equal(f.rfbs.length, 1);
});
test("disconnect release is epoch captured and stale event cannot revoke replacement", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const original = api.rfb;
  const old = f.deferRelease();
  original.emit("disconnect");
  await tick();
  const release = f.requests.find((r) => r.body?.action === "release");
  assert.equal(release.headers["X-Sidekicks-Epoch"], "1");
  assert.equal(release.keepalive, true);
  f.setEpoch(2);
  const acquireCount = f.requests.filter(
    (r) => r.body?.action === "acquire",
  ).length;
  const pending = api.take();
  await tick();
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    acquireCount,
  );
  old.resolve(f.response(409, {}));
  await pending;
  assert.equal(api.epoch, 2);
  assert.notEqual(api.rfb, original);
  await tick();
  assert.equal(api.epoch, 2);
  assert.equal(api.controlling, true);
  const count = f.requests.length;
  original.emit("disconnect");
  await tick();
  assert.equal(f.requests.length, count);
});
test("noVNC Take waits for a replacement stream before acquiring control", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  f.setAutoConnect(false);
  api.rfb.emit("disconnect");
  await tick();
  const count = f.requests.filter((r) => r.body?.action === "acquire").length;
  const pending = api.take();
  await tick();
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    count,
  );
  assert.equal(api.controlling, false);
  api.rfb.emit("connect");
  await pending;
  assert.equal(api.controlling, true);
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    count + 1,
  );
});
test("noVNC Take does not acquire when a replacement stream fails or times out", async () => {
  for (const failure of ["disconnect", "timeout"]) {
    const f = viewerFixture();
    await tick();
    const api = f.context.window.__sidekicks;
    f.setAutoConnect(false);
    api.rfb.emit("disconnect");
    await tick();
    const pending = api.take();
    const rejected = assert.rejects(
      pending,
      failure === "timeout"
        ? /timed out|Control request canceled/
        : /connection failed|Control request canceled/,
    );
    await tick();
    if (failure === "timeout") f.expireConnection();
    else api.rfb.emit("disconnect");
    await rejected;
    assert.equal(
      f.requests.some((r) => r.body?.action === "acquire"),
      false,
    );
    assert.equal(api.controlling, false);
  }
});
test("noVNC drops and releases a delayed acquire response for a disconnected stream", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  const delayed = f.deferAcquire();
  const pending = api.take();
  const rejected = assert.rejects(pending, /canceled while taking control/);
  await tick();
  api.rfb.emit("disconnect");
  delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 7 }));
  await rejected;
  const release = f.requests.find((r) => r.body?.action === "release");
  assert.equal(release.headers["X-Sidekicks-Epoch"], "7");
  assert.equal(release.keepalive, true);
  assert.equal(api.controlling, false);
});
test("noVNC closes the exact stream when acquire response is lost, rejected or malformed", async () => {
  for (const failure of ["lost", "rejected", "malformed"]) {
    const f = viewerFixture();
    await tick();
    const api = f.context.window.__sidekicks;
    const current = api.rfb;
    const delayed = f.deferAcquire();
    const pending = api.take();
    const rejected = assert.rejects(
      pending,
      failure === "lost"
        ? /network lost/
        : failure === "rejected"
          ? /HTTP 409/
          : /Invalid viewer ownership epoch/,
    );
    await tick();
    if (failure === "lost") delayed.reject(Error("network lost"));
    else if (failure === "rejected") delayed.resolve(f.response(409, {}));
    else delayed.resolve(f.response(200, { ok: true }));
    await rejected;
    assert.equal(current.disconnectCalls, 1);
    assert.equal(api.rfb, undefined);
    assert.equal(api.controlling, false);
    f.clearDeferredAcquire();
    await api.take();
    assert.notEqual(api.rfb, current);
    assert.equal(api.controlling, true);
  }
});
test("noVNC aborts a pending acquire on blur and closes its stream on rejection", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  const current = api.rfb;
  const delayed = f.deferAcquire();
  const pending = api.take();
  const rejected = assert.rejects(pending, /aborted/);
  await tick();
  f.listeners.get("blur")();
  assert.equal(delayed.signal.aborted, true);
  delayed.reject(Error("aborted"));
  await rejected;
  assert.equal(current.disconnectCalls, 1);
  assert.equal(api.rfb, undefined);
  assert.equal(api.controlling, false);
});
test("noVNC blur or pagehide cancels Take while connecting or acquiring", async () => {
  for (const event of ["blur", "pagehide"]) {
    for (const phase of ["connecting", "acquiring"]) {
      const f = viewerFixture();
      await tick();
      const api = f.context.window.__sidekicks;
      let delayed;
      if (phase === "connecting") {
        f.setAutoConnect(false);
        api.rfb.emit("disconnect");
        await tick();
      } else delayed = f.deferAcquire();
      const pending = api.take();
      const rejected = assert.rejects(pending, /Control request canceled/);
      await tick();
      f.listeners.get(event)();
      if (phase === "connecting") api.rfb.emit("connect");
      else
        delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 8 }));
      await rejected;
      const acquire = f.requests.find((r) => r.body?.action === "acquire");
      const release = f.requests.find((r) => r.body?.action === "release");
      if (phase === "connecting") {
        assert.equal(acquire, undefined);
        assert.equal(release, undefined);
      } else {
        assert.ok(acquire);
        assert.equal(release.headers["X-Sidekicks-Epoch"], "8");
        assert.equal(release.keepalive, true);
      }
      assert.equal(api.controlling, false);
    }
  }
});
test("noVNC blur cancels a Take queued behind another operation", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  const delayed = f.deferAcquire();
  const first = api.take();
  const firstRejected = assert.rejects(first, /Control request canceled/);
  await tick();
  const queued = api.take();
  const queuedRejected = assert.rejects(queued, /Control request canceled/);
  f.listeners.get("blur")();
  delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 9 }));
  await firstRejected;
  await queuedRejected;
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    1,
  );
  assert.equal(api.controlling, false);
  const release = f.requests.find((r) => r.body?.action === "release");
  assert.equal(release.headers["X-Sidekicks-Epoch"], "9");
});
test("noVNC Release, fullscreen and Escape cancel a pending Take", async () => {
  for (const action of ["release", "fullscreen", "escape"]) {
    for (const phase of ["connecting", "acquiring"]) {
      const f = viewerFixture();
      await tick();
      const api = f.context.window.__sidekicks;
      let deferred;
      if (phase === "connecting") {
        f.setAutoConnect(false);
        api.rfb.emit("disconnect");
        await tick();
      } else deferred = f.deferAcquire();
      const pending = api.take();
      const canceled = assert.rejects(pending, /Control request canceled/);
      await tick();
      let operation;
      if (action === "escape") {
        f.context.document.fullscreenElement = {};
        let prevented = false;
        operation = f.listeners.get("keydown")({
          key: "Escape",
          preventDefault() {
            prevented = true;
          },
          stopImmediatePropagation() {},
        });
        assert.equal(prevented, true);
      } else operation = f.context.document.getElementById(action).onclick();
      if (phase === "acquiring") {
        assert.equal(deferred.signal.aborted, true);
        deferred.resolve(f.response(200, { ok: true, owned: true, epoch: 17 }));
      }
      await canceled;
      await operation;
      assert.equal(api.controlling, false);
      if (phase === "acquiring") {
        const release = f.requests.find((r) => r.body?.action === "release");
        assert.equal(release.headers["X-Sidekicks-Epoch"], "17");
      } else {
        assert.equal(
          f.requests.some((r) => r.body?.action === "acquire"),
          false,
        );
      }
      if (action === "fullscreen") assert.equal(f.fullscreen.entered, 1);
      if (action === "escape") assert.equal(f.fullscreen.exited, 1);
    }
  }
});
test("noVNC user actions do not wait for a stalled startup RFB handshake", async () => {
  for (const action of ["release", "fullscreen", "escape"]) {
    const f = viewerFixture({ connectOnStart: false });
    await tick();
    const api = f.context.window.__sidekicks;
    assert.ok(api.rfb);
    const pending = api.take();
    const canceled = assert.rejects(pending, /Control request canceled/);
    await tick();
    let operation;
    if (action === "escape") {
      f.context.document.fullscreenElement = {};
      operation = f.listeners.get("keydown")({
        key: "Escape",
        preventDefault() {},
        stopImmediatePropagation() {},
      });
    } else operation = f.context.document.getElementById(action).onclick();
    await canceled;
    await operation;
    assert.equal(
      f.requests.some((r) => r.body?.action === "acquire"),
      false,
    );
    assert.equal(api.controlling, false);
    if (action === "fullscreen") assert.equal(f.fullscreen.entered, 1);
    if (action === "escape") assert.equal(f.fullscreen.exited, 1);
    api.rfb.emit("disconnect");
    await tick();
  }
});
test("noVNC failed manual release closes its stream and blocks reacquisition", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const stream = api.rfb;
  const deferred = f.deferManualRelease();
  const pending = api.release();
  const rejected = assert.rejects(pending, /release lost/);
  await tick();
  deferred.reject(Error("release lost"));
  await rejected;
  assert.equal(stream.disconnectCalls, 1);
  assert.equal(api.controlling, false);
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    1,
  );
});
test("noVNC manual release times out, closes its stream and blocks reacquisition", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const stream = api.rfb;
  f.deferManualRelease();
  const pending = api.release();
  const rejected = assert.rejects(pending, /release aborted/);
  await tick();
  f.expireRelease();
  await rejected;
  assert.equal(stream.disconnectCalls, 1);
  assert.equal(api.controlling, false);
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
});
test("a confirmed stale manual release does not block a fresh Take", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const deferred = f.deferManualRelease();
  const pending = api.release();
  const rejected = assert.rejects(pending, /HTTP 409/);
  await tick();
  deferred.resolve(f.response(409, {}));
  await rejected;
  f.setEpoch(2);
  await api.take();
  assert.equal(api.controlling, true);
  assert.equal(api.epoch, 2);
});
test("blur keepalive cannot release a newer epoch and protected input carries epoch", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const old = f.deferRelease();
  f.listeners.get("blur")();
  await tick();
  f.setEpoch(2);
  const count = f.requests.filter((r) => r.body?.action === "acquire").length;
  const pending = api.take();
  await tick();
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    count,
  );
  old.resolve(f.response(409, {}));
  await pending;
  await tick();
  assert.equal(api.epoch, 2);
  assert.equal(api.controlling, true);
  await api.api("/input", { events: [{ type: "releaseAll" }] });
  assert.equal(f.requests.at(-1).headers["X-Sidekicks-Epoch"], "2");
});
test("noVNC blocks Take after an unconfirmed background release", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const old = f.deferRelease();
  f.listeners.get("blur")();
  await tick();
  old.resolve(f.response(500, {}));
  await tick();
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    1,
  );
});

test("real-viewer takeover assertion fails on the first owned-but-held observation", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const heldCode = source.slice(
    source.indexOf("function held(state)"),
    source.indexOf("function safeState(state)"),
  );
  const takeCode = source.slice(
    source.indexOf("async function take(page"),
    source.indexOf("async function release(page"),
  );
  let observations = 0;
  const state = {
    control: { owner: "human" },
    actual: { Keys: Array(32).fill(0), Buttons: 1 },
  };
  state.actual.Keys[6] = 4;
  const context = {
    assert: (ok, message) => assert.ok(ok, message),
    cli: async () => {
      observations++;
      return state;
    },
    poll: async (fn) => {
      const result = await fn();
      assert.ok(result);
      return result;
    },
    sourceInfo: async () => ({ width: 1280, height: 720 }),
  };
  vm.createContext(context);
  vm.runInContext(heldCode + takeCode, context);
  const page = {
    locator: () => ({ click: async () => {} }),
    evaluate: async () => true,
  };
  await assert.rejects(
    context.take(page, "owned", "desktop-novnc"),
    /takeover acknowledged while actual input remained held/,
  );
  assert.equal(
    observations,
    1,
    "takeover assertion hid delayed release by polling until clear",
  );
  state.actual.Keys.fill(0);
  state.actual.Buttons = 0;
  observations = 0;
  assert.equal(await context.take(page, "owned", "desktop-neko"), state);
  assert.equal(observations, 1);
});

test("E2E inventory includes stopped and other compute containers without exclusions", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const code = source.slice(
    source.indexOf("async function protectedContainers()"),
    source.indexOf("// A failed create"),
  );
  const calls = [];
  const stopped = {
    id: "aaa",
    name: "/old-stopped",
    image: "sha256:one",
    restartCount: 0,
    state: { status: "exited", running: false },
  };
  const compute = {
    id: "bbb",
    name: "/sidekicks-compute-another-session",
    image: "sha256:two",
    restartCount: 2,
    state: { status: "running", running: true },
  };
  const context = {
    assert: (ok, message) => assert.ok(ok, message),
    dockerArgs: ["--host", "unix:///mock.sock"],
    root: "/mock",
    execFileAsync: async (command, args) => {
      calls.push({ command, args });
      if (args.includes("ps")) {
        assert.ok(args.includes("-a"));
        return {
          stdout: "aaa\told-stopped\nbbb\tsidekicks-compute-another-session\n",
        };
      }
      assert.ok(args.includes("inspect"));
      assert.ok(args.includes("aaa"));
      assert.ok(args.includes("bbb"));
      return {
        stdout: JSON.stringify(compute) + "\n" + JSON.stringify(stopped) + "\n",
      };
    },
  };
  vm.createContext(context);
  vm.runInContext(code, context);
  const result = await context.protectedContainers();
  assert.equal(result.length, 2);
  assert.equal(result[0].state.status, "exited");
  assert.equal(result[1].name, compute.name);
  assert.equal(calls.length, 2);
});
test("failed or unconfirmed create never issues name-based stop or destroy", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const code = source.slice(
    source.indexOf("async function cleanupAcknowledged("),
    source.indexOf("function held(state)"),
  );
  const calls = [];
  const context = {
    cli: async (operation, id) => {
      calls.push({ operation, id });
      return { ok: true };
    },
    redact: (value) => String(value),
  };
  vm.createContext(context);
  vm.runInContext(code, context);
  const skipped = await context.cleanupAcknowledged(
    "sidekicks-compute-colliding-existing",
    false,
  );
  assert.equal(skipped.skippedUnconfirmedCreation, true);
  assert.equal(calls.length, 0);
  const confirmed = await context.cleanupAcknowledged(
    "sidekicks-compute-created-by-this-run",
    true,
  );
  assert.equal(confirmed.stopped, true);
  assert.equal(confirmed.destroyed, true);
  assert.deepEqual(calls, [
    { operation: "stop", id: "sidekicks-compute-created-by-this-run" },
    { operation: "destroy", id: "sidekicks-compute-created-by-this-run" },
  ]);
});
test("lifecycle traces contain operations and outcomes, never request payloads", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const code = source.slice(
    source.indexOf("async function cli("),
    source.indexOf("async function protectedContainers()"),
  );
  const entries = [],
    contexts = new Map([["generated", { entries, persist: async () => {} }]]);
  let fail = false;
  const context = {
    lifecycleTraces: contexts,
    lifecycleOperations: new Set(["create", "start", "stop", "destroy"]),
    Date,
    cliPath: "/mock/cli",
    socket: "/mock.sock",
    root: "/mock",
    redact: () => "redacted",
    execFileAsync: async () => {
      if (fail) throw Error("request secret");
      return { stdout: '{"ok":true}' };
    },
  };
  vm.createContext(context);
  vm.runInContext(code, context);
  await context.cli("create", "generated", ["--request", "secret"]);
  fail = true;
  await assert.rejects(context.cli("start", "generated"));
  assert.equal(entries.length, 2);
  assert.equal(entries[0].success, true);
  assert.equal(entries[1].success, false);
  for (const entry of entries) {
    assert.deepEqual(Object.keys(entry).sort(), [
      "at",
      "finishedAt",
      "operation",
      "success",
      "target",
    ]);
    assert.ok(!JSON.stringify(entry).includes("secret"));
  }
});

test("fresh fixture click calibrates actual pointer before using window-derived targets", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const stateCode = source.slice(
    source.indexOf("function held(state)"),
    source.indexOf("async function waitReady("),
  );
  const fixtureCode = source.slice(
    source.indexOf("function contentOrigin("),
    source.indexOf("async function connectPage("),
  );
  let reads = 0;
  const clicks = [],
    moves = [],
    telemetry = [];
  const fixture = {
    instance: "fresh",
    rects: { typing: { x: 33, y: 161, width: 291, height: 86 } },
    pointer: null,
    window: {
      screenX: -32000,
      screenY: -32000,
      outerWidth: 0,
      outerHeight: 0,
      innerWidth: 1076,
      innerHeight: 417,
    },
  };
  const state = () => ({
    ready: true,
    control: { owner: "human" },
    actual: { Keys: Array(32).fill(0), Buttons: 0, X: 300, Y: 250 },
    fixture: reads === 1 ? null : fixture,
  });
  const context = {
    assert: (ok, message) => assert.ok(ok, message),
    Number,
    Math,
    Date,
    JSON,
    path: { join: (...parts) => parts.join("/") },
    pause: async () => {},
    point: async (_page, _adapter, x, y) => ({ x, y }),
    physicalURL: async () => {},
    writeFile: async (file, data) => {
      telemetry.push(JSON.parse(data));
    },
    cli: async () => {
      reads++;
      return state();
    },
    poll: async (fn) => {
      for (let i = 0; i < 3; i++) {
        const value = await fn();
        if (value) return value;
      }
      throw Error("mock not ready");
    },
    remoteClick: async (page, adapter, x, y) => {
      clicks.push({ x, y });
      if (clicks.length === 2)
        fixture.pointer = {
          screenX: 300,
          screenY: 250,
          clientX: 198,
          clientY: 88,
        };
      if (clicks.length === 3) throw Error("stop-after-calibrated-target");
    },
  };
  vm.createContext(context);
  vm.runInContext(stateCode + fixtureCode, context);
  const page = {
    keyboard: { press: async () => {} },
    mouse: { move: async (x, y) => moves.push({ x, y }) },
  };
  await assert.rejects(
    context.fixtureProof(page, "owned", "desktop-novnc", "/mock", "after"),
    /stop-after-calibrated-target/,
  );
  assert.deepEqual(clicks, [
    { x: 300, y: 250 },
    { x: 300, y: 250 },
    { x: 235, y: 343 },
  ]);
  assert.deepEqual(moves, [{ x: 292, y: 242 }]);
  const last = telemetry.at(-1).at(-1);
  assert.equal(last.stage, "calibrated-textarea-target");
  assert.equal(last.origin.source, "actual-pointer");
  assert.equal(last.state.fixture.window.screenX, -32000);
});
