import RFB from "/vendor/novnc/core/rfb.js";
import { DesktopInput, streamGeometry } from "/input.mjs";
const $ = (id) => document.getElementById(id);
let rfb,
  input,
  streamPromise,
  streamClosing = false,
  activeTakeAbort,
  activeTakeWaitCancel,
  authenticationReady,
  controlled = false,
  epoch = 0,
  streamEpoch = 0,
  streamGeneration = 0,
  controlIntent = 0,
  sequence = Promise.resolve(),
  cleanupBarrier = Promise.resolve();
const status = (s) => ($("status").textContent = s);
function clearLocal(expected) {
  if (epoch !== expected) return;
  controlled = false;
  epoch = 0;
  input?.suspend();
  if (rfb) rfb.viewOnly = true;
}
function epochRequired(path, body) {
  return (
    path === "/input" ||
    (path === "/control" && ["renew", "release", "stop"].includes(body?.action))
  );
}
const api = async (path, body, options = {}) => {
  // Capture at invocation, never after awaiting or when a queued operation runs.
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
function releaseInBackground(expected) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  const release = api(
    "/control",
    { action: "release" },
    { epoch: expected, keepalive: true, signal: controller.signal },
  )
    .finally(() => clearTimeout(timer))
    .catch((error) => {
      // A stale epoch was already rejected and cannot affect a later lease.
      if (error.status !== 409) throw error;
    });
  cleanupBarrier = Promise.all([cleanupBarrier, release]).then(() => undefined);
  // Preserve failure for the next Take without an unhandled rejection.
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
function cancelPendingTake() {
  controlIntent++;
  activeTakeAbort?.abort();
  activeTakeWaitCancel?.();
}
async function authenticate() {
  const ticket = new URLSearchParams(location.hash.slice(1)).get("ticket");
  history.replaceState(null, "", location.pathname);
  if (ticket) await api("/auth", { ticket });
}
function createInput() {
  input?.destroy();
  input = new DesktopInput({
    container: $("screen"),
    geometry: () => streamGeometry($("screen").querySelector("canvas")),
    getEpoch: () => (controlled ? epoch : 0),
    send: (events, expected) =>
      api("/input", { events }, { epoch: expected, keepalive: true }),
    onFailure: (error, expected) => {
      clearLocal(expected);
      status(error.message);
      releaseInBackground(expected);
    },
  });
}
async function connect() {
  if (rfb) {
    if (streamClosing) throw Error("Video disconnecting; retry Take");
    return streamPromise;
  }
  const generation = ++streamGeneration;
  const current = new RFB(
    $("screen"),
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/websockify`,
    { shared: true },
  );
  rfb = current;
  streamClosing = false;
  let resolveStream, rejectStream;
  let settled = false;
  streamPromise = new Promise((resolve, reject) => {
    resolveStream = resolve;
    rejectStream = reject;
  });
  const timer = setTimeout(() => {
    if (settled) return;
    settled = true;
    rejectStream(Error("Video connection timed out"));
    closeStream(current);
  }, 15000);
  streamEpoch = 0;
  current.scaleViewport = true;
  current.resizeSession = false;
  current.viewOnly = true;
  createInput();
  current.addEventListener("connect", () => {
    if (generation !== streamGeneration || rfb !== current) return;
    if (!settled) {
      settled = true;
      clearTimeout(timer);
      resolveStream(current);
    }
    input.sync();
    status(
      controlled
        ? "Human control · Go X11 · RFB video"
        : "View only · RFB video",
    );
  });
  current.addEventListener("disconnect", () => {
    if (generation !== streamGeneration || rfb !== current) return;
    cancelPendingTake();
    const expected = epoch;
    rfb = undefined;
    streamClosing = false;
    streamPromise = undefined;
    if (!settled) {
      settled = true;
      clearTimeout(timer);
      rejectStream(Error("Video connection failed"));
    }
    clearLocal(expected);
    if (expected) releaseInBackground(expected);
    status("Stream disconnected");
  });
  return streamPromise;
}
function closeStream(current) {
  if (rfb !== current || streamClosing) return;
  streamClosing = true;
  current.disconnect();
}
async function take(intent) {
  if (intent !== controlIntent) throw Error("Control request canceled");
  let cancelWait;
  const canceled = new Promise((_, reject) => {
    cancelWait = () => reject(Error("Control request canceled"));
  });
  activeTakeWaitCancel = cancelWait;
  try {
    await Promise.race([authenticationReady, canceled]);
    if (intent !== controlIntent) throw Error("Control request canceled");
    try {
      await Promise.race([cleanupBarrier, canceled]);
    } catch (error) {
      if (intent !== controlIntent) throw Error("Control request canceled");
      throw Error("Control release unconfirmed; reload viewer");
    }
    if (intent !== controlIntent) throw Error("Control request canceled");
    // The server binds control to this video stream. In particular, a Take
    // after disconnection must first establish a replacement stream.
    const current = await Promise.race([connect(), canceled]);
    if (intent !== controlIntent) throw Error("Control request canceled");
    const controller = new AbortController();
    activeTakeAbort = controller;
    let result;
    try {
      result = await api(
        "/control",
        { action: "acquire", leaseMs: 30000 },
        { signal: controller.signal },
      );
      if (
        !Number.isSafeInteger(result.epoch) ||
        result.epoch <= 0 ||
        !result.owned
      )
        throw Error("Invalid viewer ownership epoch");
    } catch (error) {
      // The guest may have acquired control even if the response was lost or
      // malformed. Closing this exact stream invokes server-side lease cleanup.
      closeStream(current);
      throw error;
    } finally {
      if (activeTakeAbort === controller) activeTakeAbort = undefined;
    }
    if (rfb !== current || intent !== controlIntent) {
      releaseInBackground(result.epoch);
      throw Error("Control request canceled while taking control");
    }
    epoch = result.epoch;
    controlled = true;
    input.activate(epoch);
    status("Human control · Go X11 · RFB video");
    return result;
  } finally {
    if (activeTakeWaitCancel === cancelWait) activeTakeWaitCancel = undefined;
  }
}
function requestTake() {
  // Capture at enqueue time: focus may change while an earlier operation runs.
  const intent = controlIntent;
  return serial(() => take(intent));
}
async function release(expected = epoch) {
  if (!expected) {
    status("View only · control request canceled");
    return;
  }
  const current = rfb;
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
      // A failed response does not confirm release. Closing this stream
      // requests backend cleanup, but only a reload may clear uncertainty.
      cleanupBarrier = Promise.reject(error);
      cleanupBarrier.catch(() => {});
      if (current) closeStream(current);
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
  // A delayed old response cannot change a replacement lease's local state.
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
    // Invalidate a Take waiting on a stream or an acquire response as well as
    // releasing a lease that has already been published locally.
    cancelPendingTake();
    if (!controlled) return;
    const expected = epoch;
    clearLocal(expected);
    // sendBeacon cannot carry the epoch header. A delayed keepalive request keeps
    // its old epoch and is rejected if a newer lease has since been acquired.
    releaseInBackground(expected);
  });
setInterval(() => {
  if (controlled)
    queue((expected) => {
      if (!controlled || epoch !== expected) return;
      return api(
        "/control",
        { action: "renew", leaseMs: 30000 },
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
  get rfb() {
    return rfb;
  },
  get controlling() {
    return controlled;
  },
  get epoch() {
    return epoch;
  },
  get streamEpoch() {
    return streamEpoch;
  },
};
authenticationReady = authenticate();
// Startup video must not occupy the user-action queue while its handshake is
// pending. Take still waits for authentication and the actual RFB connection.
authenticationReady.then(() => connect()).catch((e) => status(e.message));
