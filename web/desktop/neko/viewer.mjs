import { NekoClient } from "@m1k1o/neko";
import { DesktopInput, streamGeometry } from "../input.mjs";
const $ = (id) => document.getElementById(id);
let client,
  input,
  connectionReady,
  activeTakeAbort,
  activeTakeWaitCancel,
  viewerClosed = false,
  signalingReady = false,
  controlled = false,
  epoch = 0,
  controlIntent = 0,
  sequence = Promise.resolve(),
  cleanupBarrier = Promise.resolve();
const status = (s) => ($("status").textContent = s);
function clearLocal(expected) {
  if (epoch !== expected) return;
  controlled = false;
  epoch = 0;
  input?.suspend();
  $("media").dataset.observer = "true";
}
function epochRequired(path, body) {
  return (
    path === "/input" ||
    (path === "/control" && ["renew", "release", "stop"].includes(body?.action))
  );
}
const api = async (path, body, options = {}) => {
  const expected = options.epoch ?? epoch;
  const headers = body ? { "Content-Type": "application/json" } : {};
  if (epochRequired(path, body)) {
    if (!Number.isSafeInteger(expected) || expected <= 0)
      throw Error("No current viewer epoch");
    headers["X-Sidekicks-Epoch"] = String(expected);
  }
  const r = await fetch(path, {
    method: body ? "POST" : "GET",
    headers,
    body: body ? JSON.stringify(body) : undefined,
    keepalive: options.keepalive ?? false,
    signal: options.signal,
  });
  const text = await r.text();
  let j;
  try {
    j = JSON.parse(text);
  } catch {
    j = {};
  }
  if (!r.ok) {
    if (epochRequired(path, body) && [401, 409].includes(r.status))
      clearLocal(expected);
    const error = Error(j.error || `HTTP ${r.status}`);
    error.status = r.status;
    throw error;
  }
  return j;
};
function cancelPendingTake() {
  controlIntent++;
  activeTakeAbort?.abort();
  activeTakeWaitCancel?.();
}
function closeViewer(current) {
  if (!current || client !== current || viewerClosed) return;
  // A lost control response may have granted a lease without revealing its
  // epoch. Close only this viewer's signaling connection so the backend can
  // release its bound lease. Reacquisition requires a fresh page/session.
  viewerClosed = true;
  signalingReady = false;
  current.disconnect();
}
function releaseInBackground(expected) {
  const current = client;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  const release = api(
    "/control",
    { action: "release" },
    { epoch: expected, keepalive: true, signal: controller.signal },
  )
    .finally(() => clearTimeout(timer))
    .catch((error) => {
      if (error.status === 409) return;
      closeViewer(current);
      throw error;
    });
  cleanupBarrier = Promise.all([cleanupBarrier, release]).then(() => undefined);
  cleanupBarrier.catch(() => {});
}
function serial(fn) {
  const operation = sequence.catch(() => {}).then(fn);
  sequence = operation.catch(() => {});
  return operation;
}
function queue(fn) {
  const expected = epoch;
  const operation = serial(() => fn(expected));
  operation.catch((e) => status(e.message));
  return operation;
}
async function connect() {
  const ticket = new URLSearchParams(location.hash.slice(1)).get("ticket");
  history.replaceState(null, "", location.pathname);
  if (ticket) await api("/auth", { ticket });
  const login = await api("/viewer", {});
  client = new NekoClient({ autoplay: true });
  client.setUrl(location.origin + "/desktop/stream");
  await client.authenticate(login.token);
  client.transport.attach($("media"));
  client.mute();
  client.connect();
  // The SDK is media-only: no Overlay and no client.input/native control.
  input = new DesktopInput({
    container: $("screen"),
    geometry: () => streamGeometry($("media").querySelector("video")),
    getEpoch: () => (controlled ? epoch : 0),
    send: (events, expected) =>
      api("/input", { events }, { epoch: expected, keepalive: true }),
    onFailure: (error, expected) => {
      clearLocal(expected);
      status(error.message);
      releaseInBackground(expected);
    },
  });
  client.transport.on("cursor.image", (image) => input.setCursor(image));
  client.transport.on("cursor.position", (position) =>
    input.setCursorPosition(position),
  );
  client.store.subscribe((s) => s.screen.size, fit);
  new ResizeObserver(fit).observe($("screen"));
  fit();
  const connectedClient = client;
  client.events.on("connection.status", (s) => {
    if (client !== connectedClient) return;
    const wasReady = signalingReady;
    signalingReady = s === "connected" && !viewerClosed;
    if (wasReady && !signalingReady) {
      cancelPendingTake();
      const expected = epoch;
      clearLocal(expected);
      if (expected) releaseInBackground(expected);
    }
    status(`Neko stream · ${s}`);
  });
  client.events.on("connection.closed", () => {
    if (client !== connectedClient) return;
    viewerClosed = true;
    signalingReady = false;
    cancelPendingTake();
    const expected = epoch;
    clearLocal(expected);
    if (expected) releaseInBackground(expected);
    status("Stream disconnected");
  });
}
async function take(intent) {
  if (intent !== controlIntent) throw Error("Control request canceled");
  let cancelWait;
  const canceled = new Promise((_, reject) => {
    cancelWait = () => reject(Error("Control request canceled"));
  });
  activeTakeWaitCancel = cancelWait;
  try {
    try {
      await Promise.race([cleanupBarrier, canceled]);
    } catch (error) {
      if (intent !== controlIntent) throw Error("Control request canceled");
      throw Error("Control release unconfirmed; reload viewer");
    }
    await Promise.race([connectionReady, canceled]);
    if (intent !== controlIntent) throw Error("Control request canceled");
    const current = client;
    if (viewerClosed || !current || !signalingReady)
      throw Error("Viewer stream not connected; retry or reload viewer");
    const controller = new AbortController();
    activeTakeAbort = controller;
    let result;
    try {
      result = await api(
        "/control",
        { action: "acquire", mode: "gated", leaseMs: 30000 },
        { signal: controller.signal },
      );
      if (
        !Number.isSafeInteger(result.epoch) ||
        result.epoch <= 0 ||
        !result.owned
      )
        throw Error("Invalid viewer ownership epoch");
    } catch (error) {
      closeViewer(current);
      throw error;
    } finally {
      if (activeTakeAbort === controller) activeTakeAbort = undefined;
    }
    if (
      client !== current ||
      viewerClosed ||
      !signalingReady ||
      intent !== controlIntent
    ) {
      releaseInBackground(result.epoch);
      throw Error("Control request canceled while taking control");
    }
    epoch = result.epoch;
    controlled = true;
    input.activate(epoch);
    $("media").dataset.observer = "false";
    status("Human control · Go X11 · WebRTC media");
    return result;
  } finally {
    if (activeTakeWaitCancel === cancelWait) activeTakeWaitCancel = undefined;
  }
}
function requestTake() {
  const intent = controlIntent;
  return serial(() => take(intent));
}
async function release(expected = epoch) {
  if (!expected) {
    status("View only · control request canceled");
    return;
  }
  const current = client;
  const releasingCurrent = controlled && epoch === expected;
  clearLocal(expected);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  let result;
  try {
    result = await api(
      "/control",
      { action: "release" },
      { epoch: expected, signal: controller.signal },
    );
  } catch (error) {
    if (error.status !== 409 && releasingCurrent) {
      cleanupBarrier = Promise.reject(error);
      cleanupBarrier.catch(() => {});
      closeViewer(current);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
  if (!controlled) status("View only · inputs released");
  return result;
}
$("take").onclick = () => {
  const operation = requestTake();
  operation.catch((e) => status(e.message));
  return operation;
};
function requestRelease() {
  const expected = epoch;
  cancelPendingTake();
  return serial(() => release(expected));
}
$("release").onclick = () => {
  const operation = requestRelease();
  operation.catch((e) => status(e.message));
  return operation;
};
$("fullscreen").onclick = () => {
  cancelPendingTake();
  return queue(async (expected) => {
    if (controlled && epoch !== expected) return;
    if (controlled) await release(expected);
    if (document.fullscreenElement) await document.exitFullscreen();
    else await $("screen").requestFullscreen();
  });
};
window.addEventListener(
  "keydown",
  (e) => {
    if (e.key !== "Escape" || !document.fullscreenElement) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    cancelPendingTake();
    return queue(async (expected) => {
      if (controlled && epoch !== expected) return;
      if (controlled) await release(expected);
      await document.exitFullscreen();
    });
  },
  { capture: true },
);
for (const event of ["blur", "pagehide"])
  window.addEventListener(event, () => {
    cancelPendingTake();
    if (!controlled) return;
    const expected = epoch;
    clearLocal(expected);
    releaseInBackground(expected);
  });
setInterval(() => {
  if (controlled)
    queue((expected) => {
      if (!controlled || epoch !== expected) return;
      return api(
        "/control",
        { action: "renew", mode: "gated", leaseMs: 30000 },
        { epoch: expected },
      );
    });
}, 10000);
window.__sidekicks = {
  take: requestTake,
  release: requestRelease,
  api,
  inputMode: "gated",
  nativeInputSupported: false,
  get epoch() {
    return epoch;
  },
  get client() {
    return client;
  },
  get controlling() {
    return controlled;
  },
  async stats() {
    return [...(await client.transport.pc.getStats()).values()];
  },
};
connectionReady = connect();
connectionReady.catch((e) => status(e.message));

function fit() {
  if (!client) return;
  const s = client.state.screen.size;
  if (!s?.width) return;
  const r = $("screen").getBoundingClientRect();
  const scale = Math.min(r.width / s.width, r.height / s.height);
  Object.assign($("media").style, {
    width: s.width * scale + "px",
    height: s.height * scale + "px",
  });
  input?.sync();
}
