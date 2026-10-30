import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const tick = () => new Promise((resolve) => setImmediate(resolve));

function viewerFixture({ autoConnect = true } = {}) {
  const elements = new Map(),
    listeners = new Map(),
    requests = [],
    clients = [],
    timers = [],
    fullscreen = { entered: 0, exited: 0 };
  let responseEpoch = 1,
    deferredAcquire,
    deferredRelease,
    deferredManualRelease;
  const element = (id) => {
    if (!elements.has(id))
      elements.set(id, {
        style: {},
        dataset: {},
        textContent: "",
        querySelector: () => null,
        getBoundingClientRect: () => ({ width: 1280, height: 720 }),
        requestFullscreen: async () => {
          fullscreen.entered++;
          context.document.fullscreenElement = elements.get(id);
        },
      });
    return elements.get(id);
  };
  class NekoClient {
    constructor() {
      this.handlers = new Map();
      this.events = { on: (name, fn) => this.handlers.set(name, fn) };
      this.state = { screen: { size: { width: 1280, height: 720 } } };
      this.transport = {
        attach() {},
        on() {},
        pc: { getStats: async () => new Map() },
      };
      this.store = { subscribe() {} };
      clients.push(this);
    }
    emit(name, value) {
      this.handlers.get(name)?.(value);
    }
    setUrl() {}
    async authenticate() {}
    mute() {}
    connect() {
      if (autoConnect)
        queueMicrotask(() => this.emit("connection.status", "connected"));
    }
    disconnect() {
      this.disconnectCalls = (this.disconnectCalls || 0) + 1;
      this.emit("connection.closed");
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
    setCursor() {}
    setCursorPosition() {}
  }
  class ResizeObserver {
    observe() {}
  }
  const response = (status, body) => ({
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  });
  const context = {
    NekoClient,
    DesktopInput: Input,
    ResizeObserver,
    streamGeometry() {},
    URLSearchParams,
    Promise,
    AbortController,
    Number,
    Error,
    setTimeout(fn, ms) {
      if (ms !== 5000) return setTimeout(fn, ms);
      const timer = { fn, active: true };
      timers.push(timer);
      return timer;
    },
    clearTimeout(timer) {
      if (timer && typeof timer === "object" && "active" in timer)
        timer.active = false;
      else clearTimeout(timer);
    },
    setInterval() {},
    location: { origin: "http://127.0.0.1:1234", pathname: "/", hash: "" },
    history: { replaceState() {} },
    document: {
      getElementById: element,
      fullscreenElement: null,
      exitFullscreen: async () => {
        fullscreen.exited++;
        context.document.fullscreenElement = null;
      },
    },
    window: { addEventListener: (type, fn) => listeners.set(type, fn) },
    fetch: async (path, options = {}) => {
      const body = options.body ? JSON.parse(options.body) : null;
      requests.push({
        path,
        body,
        headers: options.headers,
        keepalive: options.keepalive,
        signal: options.signal,
      });
      if (body?.action === "acquire" && deferredAcquire)
        return new Promise((resolve, reject) => {
          deferredAcquire.resolve = resolve;
          deferredAcquire.reject = reject;
          deferredAcquire.signal = options.signal;
        });
      if (body?.action === "release" && options.keepalive && deferredRelease)
        return new Promise((resolve, reject) => {
          deferredRelease.resolve = resolve;
          deferredRelease.reject = reject;
          options.signal.addEventListener("abort", () =>
            reject(Error("aborted")),
          );
        });
      if (
        body?.action === "release" &&
        !options.keepalive &&
        deferredManualRelease
      )
        return new Promise((resolve, reject) => {
          deferredManualRelease.resolve = resolve;
          deferredManualRelease.reject = reject;
          options.signal.addEventListener("abort", () =>
            reject(Error("aborted")),
          );
        });
      if (path === "/viewer") return response(200, { token: "test-token" });
      return response(200, { ok: true, owned: true, epoch: responseEpoch });
    },
  };
  vm.createContext(context);
  const source = fs
    .readFileSync(new URL("./neko/viewer.mjs", import.meta.url), "utf8")
    .replace(/^import .*;\n/gm, "");
  vm.runInContext(source, context);
  return {
    context,
    requests,
    clients,
    listeners,
    fullscreen,
    response,
    fireTimeouts() {
      for (const timer of timers) if (timer.active) timer.fn();
    },
    setEpoch(value) {
      responseEpoch = value;
    },
    deferAcquire() {
      return (deferredAcquire = {});
    },
    clearAcquire() {
      deferredAcquire = undefined;
    },
    deferRelease() {
      return (deferredRelease = {});
    },
    deferManualRelease() {
      return (deferredManualRelease = {});
    },
  };
}

test("Neko Release, fullscreen and Escape cancel a pending Take", async () => {
  for (const action of ["release", "fullscreen", "escape"]) {
    const f = viewerFixture();
    await tick();
    const api = f.context.window.__sidekicks;
    const delayed = f.deferAcquire();
    const take = api.take();
    const rejected = assert.rejects(take, /Control request canceled/);
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
    assert.equal(delayed.signal.aborted, true);
    delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 17 }));
    await rejected;
    await operation;
    assert.equal(api.controlling, false);
    assert.equal(
      f.requests.find((r) => r.body?.action === "release").headers[
        "X-Sidekicks-Epoch"
      ],
      "17",
    );
    if (action === "fullscreen") assert.equal(f.fullscreen.entered, 1);
    if (action === "escape") assert.equal(f.fullscreen.exited, 1);
  }
});

test("Neko blur and pagehide cancel pending and queued Take", async () => {
  for (const event of ["blur", "pagehide"]) {
    const f = viewerFixture();
    await tick();
    const api = f.context.window.__sidekicks;
    const delayed = f.deferAcquire();
    const first = api.take();
    const rejected = assert.rejects(first, /Control request canceled/);
    await tick();
    const queued = api.take();
    const queuedRejected = assert.rejects(queued, /Control request canceled/);
    f.listeners.get(event)();
    assert.equal(delayed.signal.aborted, true);
    delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 9 }));
    await rejected;
    await queuedRejected;
    assert.equal(
      f.requests.filter((r) => r.body?.action === "acquire").length,
      1,
    );
    assert.equal(
      f.requests.find((r) => r.body?.action === "release").headers[
        "X-Sidekicks-Epoch"
      ],
      "9",
    );
    assert.equal(api.controlling, false);
  }
});

test("Neko uncertain acquire closes the exact viewer and blocks Take until reload", async () => {
  for (const failure of ["lost", "malformed"]) {
    const f = viewerFixture();
    await tick();
    const api = f.context.window.__sidekicks;
    const delayed = f.deferAcquire();
    const pending = api.take();
    const rejected = assert.rejects(
      pending,
      failure === "lost" ? /network lost/ : /Invalid viewer ownership epoch/,
    );
    await tick();
    if (failure === "lost") delayed.reject(Error("network lost"));
    else delayed.resolve(f.response(200, { ok: true }));
    await rejected;
    assert.equal(f.clients[0].disconnectCalls, 1);
    assert.equal(api.controlling, false);
    f.clearAcquire();
    await assert.rejects(api.take(), /Viewer stream not connected/);
  }
});

test("Neko failed manual release closes its viewer and fences reacquisition", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const delayed = f.deferManualRelease();
  const pending = api.release();
  const rejected = assert.rejects(pending, /release lost/);
  await tick();
  delayed.reject(Error("release lost"));
  await rejected;
  assert.equal(f.clients[0].disconnectCalls, 1);
  assert.equal(api.controlling, false);
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    1,
  );
});

test("Neko hung manual release times out and fences reacquisition", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  f.deferManualRelease();
  const pending = api.release();
  const rejected = assert.rejects(pending, /aborted/);
  await tick();
  f.fireTimeouts();
  await rejected;
  assert.equal(f.clients[0].disconnectCalls, 1);
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
});

test("Neko waits for an old blur release before a fresh epoch", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  const delayed = f.deferRelease();
  f.listeners.get("blur")();
  await tick();
  f.setEpoch(2);
  const next = api.take();
  await tick();
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    1,
  );
  delayed.resolve(f.response(409, {}));
  await next;
  assert.equal(api.epoch, 2);
  assert.equal(api.controlling, true);
  assert.equal(
    f.requests.find((r) => r.body?.action === "release").headers[
      "X-Sidekicks-Epoch"
    ],
    "1",
  );
});

test("Neko hung blur release closes the viewer and fences reacquisition", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  await api.take();
  f.deferRelease();
  f.listeners.get("blur")();
  await tick();
  f.fireTimeouts();
  await tick();
  assert.equal(f.clients[0].disconnectCalls, 1);
  await assert.rejects(api.take(), /release unconfirmed; reload viewer/);
});

test("Neko rejects Take until signaling is connected", async () => {
  const f = viewerFixture({ autoConnect: false });
  await tick();
  const api = f.context.window.__sidekicks;
  await assert.rejects(api.take(), /Viewer stream not connected/);
  assert.equal(
    f.requests.filter((r) => r.body?.action === "acquire").length,
    0,
  );
  f.clients[0].emit("connection.status", "connected");
  await api.take();
  assert.equal(api.controlling, true);
});

test("Neko signaling loss cancels pending Take and releases a late grant", async () => {
  const f = viewerFixture();
  await tick();
  const api = f.context.window.__sidekicks;
  const delayed = f.deferAcquire();
  const pending = api.take();
  const rejected = assert.rejects(pending, /Control request canceled/);
  await tick();
  f.clients[0].emit("connection.status", "connecting");
  assert.equal(delayed.signal.aborted, true);
  delayed.resolve(f.response(200, { ok: true, owned: true, epoch: 11 }));
  await rejected;
  assert.equal(api.controlling, false);
  assert.equal(
    f.requests.find((r) => r.body?.action === "release").headers[
      "X-Sidekicks-Epoch"
    ],
    "11",
  );
});
