// Explicit, destructive-only-to-owned-disposables real desktop verification.
// Never run against a POC/container supplied by the caller.
import { chromium } from "../../web/node_modules/playwright/index.mjs";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { randomBytes, createHash } from "node:crypto";
import {
  makeToneWAV,
  verifyTone,
  validateImageSelection,
} from "./audio-analysis.mjs";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { cleanupOwnedBrowserServer } from "./owned-child.mjs";
import { runSequential } from "./sequence.mjs";

const execFileAsync = promisify(execFile);
const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const cliPath = path.join(root, ".run/compute-bin/sidekicks");
const socket = process.env.SIDEKICKS_DOCKER_SOCKET || "/var/run/docker.sock";
if (!path.isAbsolute(socket) || socket.includes("\0"))
  throw Error("A local absolute Docker socket is required");
const dockerArgs = ["--host", `unix://${socket}`];
const args = process.argv.slice(2);
const hasImageOverride = args.includes("--image");
const imageOverride = hasImageOverride
  ? args[args.indexOf("--image") + 1]
  : null;
const audioEnabled = args.includes("--audio");
const requested = args.includes("--adapter")
  ? args[args.indexOf("--adapter") + 1]
  : "all";
if (!args.includes("--execute") || process.env.SIDEKICKS_E2E !== "1") {
  throw Error(
    "Execution requires both --execute and SIDEKICKS_E2E=1 after image handoff",
  );
}
if (!new Set(["all", "desktop-novnc", "desktop-neko"]).has(requested))
  throw Error("Unknown adapter");
if (hasImageOverride) validateImageSelection(imageOverride, requested);
if (
  args.filter((a) => a === "--image").length > 1 ||
  args.filter((a) => a === "--adapter").length > 1
)
  throw Error("Duplicate image/adapter selection");
if (audioEnabled && requested !== "desktop-neko")
  throw Error("--audio requires explicit desktop-neko adapter");
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--execute" || args[i] === "--audio") continue;
  if (args[i] === "--adapter" || args[i] === "--image") {
    i++;
    continue;
  }
  throw Error("Unknown argument");
}
const targets =
  requested === "all" ? ["desktop-novnc", "desktop-neko"] : [requested];
const secrets = new Set();
const lifecycleTraces = new Map();
const lifecycleOperations = new Set(["create", "start", "stop", "destroy"]);
// The comparison supervisor requests cooperative cancellation; normal cleanup
// remains in run()'s finally block. It may still have to force termination if a
// pending operation does not return within its grace period.
let cancellationRequested = false;
process.on("SIGTERM", () => {
  cancellationRequested = true;
});
function checkCancellation() {
  if (cancellationRequested)
    throw Error("Cancellation requested by supervisor");
}
function redact(value) {
  let text = String(value);
  for (const secret of secrets)
    if (secret) text = text.split(secret).join("[redacted]");
  return text
    .replace(/#ticket=[^\s"'<>]+/g, "#ticket=[redacted]")
    .slice(0, 4000);
}
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function bounded(promise, ms, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(Error(`${label} timed out`)), ms);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}
async function poll(
  fn,
  { timeout = 10000, interval = 120, label = "condition" } = {},
) {
  const until = Date.now() + timeout;
  let last;
  while (Date.now() < until) {
    checkCancellation();
    try {
      const result = await fn();
      if (result) return result;
    } catch (e) {
      last = e;
    }
    await pause(interval);
  }
  throw Error(
    `${label} not established${last ? `: ${redact(last.message)}` : ""}`,
  );
}
function assert(ok, message) {
  if (!ok) throw Error(message);
}
async function cli(
  command,
  id,
  extra = [],
  { binary = false, timeout = 75000 } = {},
) {
  const context = lifecycleTraces.get(id);
  const trace =
    context && lifecycleOperations.has(command)
      ? {
          operation: command,
          target: id,
          at: new Date().toISOString(),
          success: null,
        }
      : null;
  const persist = async () => {
    if (!trace) return;
    try {
      await context.persist();
    } catch {
      context.persistenceFailed = true;
    }
  };
  if (trace) {
    context.entries.push(trace);
    await persist();
  }
  let succeeded = false;
  try {
    const r = await execFileAsync(
      cliPath,
      [command, "--socket", socket, "--id", id, ...extra],
      {
        cwd: root,
        encoding: binary ? "buffer" : "utf8",
        maxBuffer: 4 << 20,
        timeout,
      },
    );
    const value = binary ? r.stdout : JSON.parse(r.stdout);
    succeeded = true;
    return value;
  } catch (e) {
    throw Error(`CLI ${command} failed: ${redact(e.stderr || e.message)}`);
  } finally {
    if (trace) {
      trace.success = succeeded;
      trace.finishedAt = new Date().toISOString();
      await persist();
    }
  }
}
async function protectedContainers() {
  const { stdout } = await execFileAsync(
    "docker",
    [
      ...dockerArgs,
      "ps",
      "-a",
      "--no-trunc",
      "--format",
      "{{.ID}}\t{{.Names}}",
    ],
    { cwd: root, timeout: 10000, maxBuffer: 1 << 20 },
  );
  const protectedIDs = stdout
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => line.split("\t")[0]);
  assert(
    protectedIDs.length <= 2048,
    "Container audit exceeds bounded inventory size",
  );
  if (!protectedIDs.length) return [];
  const inspected = await execFileAsync(
    "docker",
    [
      ...dockerArgs,
      "inspect",
      "--format",
      '{"id":{{json .Id}},"name":{{json .Name}},"image":{{json .Image}},"restartCount":{{.RestartCount}},"state":{"status":{{json .State.Status}},"running":{{.State.Running}},"paused":{{.State.Paused}},"restarting":{{.State.Restarting}},"dead":{{.State.Dead}},"exitCode":{{.State.ExitCode}},"oomKilled":{{.State.OOMKilled}},"startedAt":{{json .State.StartedAt}},"finishedAt":{{json .State.FinishedAt}}}}',
      ...protectedIDs,
    ],
    { cwd: root, timeout: 10000, maxBuffer: 1 << 20 },
  );
  return inspected.stdout
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line))
    .sort((a, b) => a.id.localeCompare(b.id));
}
// A failed create is not evidence of owning the chosen name. Provision's
// immutable-engine-ID cleanup is authoritative; the harness must not resolve
// that unconfirmed name and accidentally stop an existing colliding container.
async function cleanupAcknowledged(id, acknowledged) {
  if (!acknowledged) return { skippedUnconfirmedCreation: true, errors: [] };
  const result = { errors: [] };
  try {
    await cli("stop", id);
    result.stopped = true;
  } catch (error) {
    result.errors.push(redact(error.message));
  }
  try {
    await cli("destroy", id);
    result.destroyed = true;
  } catch (error) {
    result.errors.push(redact(error.message));
  }
  return result;
}
function held(state) {
  const actual = state?.actual;
  assert(
    actual &&
      Array.isArray(actual.Keys) &&
      actual.Keys.length === 32 &&
      Number.isInteger(actual.Buttons),
    "Actual X11 state unavailable",
  );
  return { keys: actual.Keys.some((v) => v !== 0), buttons: actual.Buttons };
}
function safeState(state) {
  return {
    ready: state.ready,
    owner: state.control?.owner ?? null,
    actual: state.actual,
    fixture: state.fixture
      ? {
          instance: state.fixture.instance,
          text: state.fixture.text,
          counts: state.fixture.counts,
          heldKeys: state.fixture.heldKeys,
          rects: state.fixture.rects,
          window: state.fixture.window,
          contentOrigin: state.fixture.contentOrigin,
          pointer: state.fixture.pointer,
        }
      : null,
  };
}
async function waitReady(id) {
  return poll(
    async () => {
      const h = await cli("health", id);
      const s = await cli("state", id);
      return h && s.ready && s;
    },
    { timeout: 25000, interval: 250, label: "guest readiness" },
  );
}
async function inspectedAllocation(id, adapter, expected) {
  return poll(
    async () => {
      const observed = await cli("inspect", id);
      assert(
        observed.id === expected.id && observed.image === expected.image,
        "Inspected workload identity/image changed",
      );
      if (!Array.isArray(observed.endpoints)) return false;
      const viewer = observed.endpoints.find(
        (p) =>
          p.guest === 6080 &&
          p.protocol === "tcp" &&
          p.host === "127.0.0.1" &&
          p.port > 0,
      );
      if (!viewer) return false;
      if (adapter === "desktop-neko") {
        const udp = observed.endpoints.filter(
          (p) =>
            p.protocol === "udp" &&
            p.guest >= 60020 &&
            p.guest < 60040 &&
            p.host === "127.0.0.1" &&
            p.port > 0,
        );
        if (udp.length !== 20 || new Set(udp.map((p) => p.guest)).size !== 20)
          return false;
      }
      return observed;
    },
    { timeout: 10000, label: "current inspected viewer/media allocation" },
  );
}
async function homeVisual(page, adapter) {
  return page.evaluate((kind) => {
    const source = document.querySelector(
      kind === "desktop-novnc" ? "#screen canvas" : "#media video",
    );
    if (!source) return null;
    const c = document.createElement("canvas");
    c.width = 1280;
    c.height = 720;
    c.getContext("2d").drawImage(source, 0, 0, 1280, 720);
    const data = c.getContext("2d").getImageData(180, 220, 820, 290).data;
    let bright = 0,
      cyan = 0,
      dark = 0;
    for (let i = 0; i < data.length; i += 4) {
      const r = data[i],
        g = data[i + 1],
        b = data[i + 2];
      if (Math.min(r, g, b) > 185) bright++;
      if (
        r > 5 &&
        r < 100 &&
        g > 100 &&
        g < 230 &&
        b > 130 &&
        b < 250 &&
        b > r + 70
      )
        cyan++;
      if (Math.max(r, g, b) < 110) dark++;
    }
    return {
      brightFraction: bright / (data.length / 4),
      cyanIconPixels: cyan,
      darkGlyphPixels: dark,
    };
  }, adapter);
}
async function sourceInfo(page, adapter) {
  return page.evaluate((kind) => {
    const element = document.querySelector(
      kind === "desktop-novnc" ? "#screen canvas" : "#media video",
    );
    if (!element) return null;
    const width =
      element instanceof HTMLVideoElement ? element.videoWidth : element.width;
    const height =
      element instanceof HTMLVideoElement
        ? element.videoHeight
        : element.height;
    const r = element.getBoundingClientRect();
    if (!width || !height || !r.width || !r.height) return null;
    const scale = Math.min(r.width / width, r.height / height);
    return {
      width,
      height,
      x: r.x + (r.width - width * scale) / 2,
      y: r.y + (r.height - height * scale) / 2,
      scale,
    };
  }, adapter);
}
async function point(page, adapter, x, y) {
  const source = await sourceInfo(page, adapter);
  assert(
    source?.width === 1280 && source.height === 720,
    "Unexpected decoded desktop geometry",
  );
  assert(
    x >= 0 && y >= 0 && x < 1280 && y < 720,
    `Remote point outside desktop: requested (${String(x)}, ${String(y)}), decoded ${source.width}x${source.height}`,
  );
  return { x: source.x + x * source.scale, y: source.y + y * source.scale };
}
async function remoteClick(page, adapter, x, y) {
  const p = await point(page, adapter, x, y);
  await page.mouse.click(p.x, p.y, { delay: 40 });
}
async function physicalURL(page, text) {
  // Ordinary browser keyboard events with explicit physical Shift for ':' avoid
  // misclassifying native SDK modifier translation as a browser Home failure.
  for (const ch of text) {
    if (ch === ":") {
      await page.keyboard.down("Shift");
      await page.keyboard.press("Semicolon", { delay: 25 });
      await page.keyboard.up("Shift");
      await pause(15);
    } else await page.keyboard.type(ch, { delay: 16 });
  }
}
async function take(page, id, adapter) {
  await page.locator("#take").click();
  const acknowledged = await poll(
    async () => {
      const s = await cli("state", id);
      const local = await page.evaluate(
        (kind) =>
          !!window.__sidekicks?.controlling &&
          (kind !== "desktop-neko" ||
            (window.__sidekicks?.inputMode === "gated" &&
              !window.__sidekicks?.client?.controlling)),
        adapter,
      );
      return local && s.control?.owner === "human" && s;
    },
    { label: "actual human ownership" },
  );
  const actual = held(acknowledged);
  assert(
    !actual.keys && actual.buttons === 0,
    "Human takeover acknowledged while actual input remained held",
  );
  await poll(
    async () => {
      const v = await sourceInfo(page, adapter);
      return v?.width === 1280 && v.height === 720;
    },
    { label: "decoded desktop after takeover" },
  );
  return acknowledged;
}
async function release(page, id) {
  // Invoke the exact viewer UI release function without sending a local mouse-up
  // first. This proves server release while real transport input is still held.
  await page.evaluate(() => window.__sidekicks.release());
  return poll(
    async () => {
      const s = await cli("state", id);
      const a = held(s);
      return !s.control?.owner && !a.keys && a.buttons === 0 && s;
    },
    { label: "server release of all actual input" },
  );
}
// Exercise the observer-first path with a separate owned viewer. Block its
// JavaScript control and input requests before closing the page, so only server
// WebSocket-disconnect cleanup can revoke the lease. This is a page/socket loss
// test, not a claim about an OS process crash or physical input devices.
async function observerDisconnectProof(browser, id, instance, beforeProof) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 920 },
    deviceScaleFactor: 1,
  });
  let blockedRequests = 0;
  try {
    const connection = await cli("connect", id);
    assert(
      typeof connection.url === "string" &&
        /^http:\/\/127\.0\.0\.1:\d+\/#ticket=[A-Za-z0-9_-]{43}$/.test(
          connection.url,
        ),
      "Invalid private observer URL",
    );
    secrets.add(connection.url);
    secrets.add(connection.url.split("ticket=")[1]);
    const page = await context.newPage();
    await page.goto(connection.url, { waitUntil: "domcontentloaded" });
    await poll(
      () =>
        page.evaluate(
          () =>
            !location.hash &&
            window.__sidekicks?.rfb?._rfbConnectionState === "connected" &&
            !window.__sidekicks.controlling,
        ),
      { label: "observer stream connected before ownership" },
    );
    const initial = await cli("state", id);
    assert(!initial.control?.owner, "Observer test requires unowned desktop");
    await take(page, id, "desktop-novnc");
    const target = await point(
      page,
      "desktop-novnc",
      beforeProof.origin.x + beforeProof.rect.x + 80,
      beforeProof.origin.y + beforeProof.rect.y + 22,
    );
    await page.mouse.move(target.x, target.y);
    await page.mouse.down();
    await poll(
      async () => {
        const state = await cli("state", id);
        return (held(state).buttons & 256) !== 0 && !held(state).keys;
      },
      { label: "observer test actual left button down" },
    );
    await page.keyboard.down("Shift");
    const before = await poll(
      async () => {
        const state = await cli("state", id);
        const actual = held(state);
        return (
          state.control?.owner === "human" &&
          actual.keys &&
          (actual.buttons & 256) !== 0 &&
          state
        );
      },
      { label: "observer test actual Shift and button held" },
    );
    // Block both control/release and input/releaseAll. Replace fetch in the
    // page as well as routing, because an unload keepalive may outlive the
    // page before Playwright's interception gets a chance to observe it.
    await page.evaluate(() => {
      const original = window.fetch;
      window.fetch = function (input, init) {
        const url = new URL(
          typeof input === "string" ? input : input.url,
          location.href,
        );
        if (
          url.origin === location.origin &&
          ["/control", "/input"].includes(url.pathname)
        )
          return Promise.reject(Error("E2E blocks viewer cleanup requests"));
        return original.call(this, input, init);
      };
    });
    for (const path of ["**/control", "**/input"]) {
      await context.route(path, async (route) => {
        blockedRequests++;
        await route.abort("blockedbyclient");
      });
    }
    await bounded(
      page.close({ runBeforeUnload: false }),
      5000,
      "observer page close",
    );
    const after = await poll(
      async () => {
        const state = await cli("state", id);
        const actual = held(state);
        return (
          !state.control?.owner && !actual.keys && actual.buttons === 0 && state
        );
      },
      {
        timeout: 5000,
        label: "server disconnect releases owner and actual X11 input",
      },
    );
    return {
      before: safeState(before),
      after: safeState(after),
      blockedRequests,
      cleanupPath:
        "observer page closed; its control and input requests blocked",
      image: instance.image,
    };
  } finally {
    await bounded(context.close(), 5000, "observer context close");
  }
}
async function media(page, adapter, instance) {
  const info = await sourceInfo(page, adapter);
  assert(
    info?.width === 1280 && info.height === 720,
    "No actual 1280x720 decoded stream",
  );
  if (adapter === "desktop-novnc")
    return {
      protocol: "RFB",
      width: 1280,
      height: 720,
      freshness: "decoded pixel agreement checked separately",
    };
  const reports = await page.evaluate(() => window.__sidekicks.stats());
  const transport = reports.find(
    (r) => r.type === "transport" && r.selectedCandidatePairId,
  );
  const pair = reports.find((r) => r.id === transport?.selectedCandidatePairId);
  const remote = reports.find((r) => r.id === pair?.remoteCandidateId);
  const local = reports.find((r) => r.id === pair?.localCandidateId);
  const inbound = reports.find(
    (r) =>
      r.type === "inbound-rtp" &&
      (r.kind === "video" || r.mediaType === "video"),
  );
  const codec = reports.find((r) => r.id === inbound?.codecId);
  assert(
    Array.isArray(instance.endpoints),
    "Current inspected media allocation unavailable",
  );
  const allocated = instance.endpoints
    .filter((p) => p.protocol === "udp" && p.guest >= 60020 && p.guest < 60040)
    .map((p) => p.port);
  assert(
    pair?.state === "succeeded" &&
      remote?.protocol?.toLowerCase() === "udp" &&
      local?.protocol?.toLowerCase() === "udp",
    "Selected ICE transport is not genuine UDP",
  );
  assert(
    (remote.address || remote.ip) === "127.0.0.1" &&
      allocated.includes(remote.port),
    "Selected ICE peer escaped allocated loopback media ports",
  );
  assert(
    inbound?.framesDecoded > 0 &&
      codec?.mimeType?.toLowerCase() === "video/vp8",
    "No actual decoded VP8 video",
  );
  return {
    protocol: "UDP",
    remoteAddress: remote.address || remote.ip,
    remotePort: remote.port,
    localProtocol: local.protocol,
    framesDecoded: inbound.framesDecoded,
    framesReceived: inbound.framesReceived,
    codec: codec.mimeType,
    width: info.width,
    height: info.height,
  };
}
async function decodedPNG(page, adapter) {
  return Buffer.from(
    await page.evaluate((kind) => {
      const source = document.querySelector(
        kind === "desktop-novnc" ? "#screen canvas" : "#media video",
      );
      const canvas = document.createElement("canvas");
      canvas.width = 1280;
      canvas.height = 720;
      canvas.getContext("2d").drawImage(source, 0, 0, 1280, 720);
      return canvas.toDataURL("image/png").split(",")[1];
    }, adapter),
    "base64",
  );
}
async function pixelAgreement(page, adapter, native, roi) {
  return page.evaluate(
    async ({ kind, encoded, roi }) => {
      const raw = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
      const image = await createImageBitmap(
        new Blob([raw], { type: "image/png" }),
      );
      if (image.width !== 1280 || image.height !== 720)
        throw Error("Native capture geometry mismatch");
      const expected = document.createElement("canvas"),
        actual = document.createElement("canvas");
      expected.width = actual.width = 1280;
      expected.height = actual.height = 720;
      expected.getContext("2d").drawImage(image, 0, 0);
      image.close();
      const source = document.querySelector(
        kind === "desktop-novnc" ? "#screen canvas" : "#media video",
      );
      actual.getContext("2d").drawImage(source, 0, 0, 1280, 720);
      const a = expected
        .getContext("2d")
        .getImageData(roi.x, roi.y, roi.width, roi.height).data;
      const b = actual
        .getContext("2d")
        .getImageData(roi.x, roi.y, roi.width, roi.height).data;
      let ink = 0,
        matched = 0,
        sum = 0;
      // White text pixels inside the first textarea line, excluding its frame and
      // cursor, make an old blank frame fail despite a mostly identical background.
      for (let i = 0; i < a.length; i += 4) {
        if (a[i] < 170 || a[i + 1] < 170 || a[i + 2] < 170) continue;
        ink++;
        const errors = [
          Math.abs(a[i] - b[i]),
          Math.abs(a[i + 1] - b[i + 1]),
          Math.abs(a[i + 2] - b[i + 2]),
        ];
        sum += errors.reduce((x, y) => x + y, 0) / 3;
        if (Math.max(...errors) <= 65) matched++;
      }
      return {
        inkPixels: ink,
        matchedInkFraction: ink ? matched / ink : 0,
        meanInkError: ink ? sum / ink : null,
      };
    },
    { kind: adapter, encoded: native.toString("base64"), roi },
  );
}
function contentOrigin(fixture) {
  const pointer = fixture.pointer;
  assert(
    pointer &&
      [
        pointer.screenX,
        pointer.screenY,
        pointer.clientX,
        pointer.clientY,
      ].every(Number.isFinite),
    "Actual fixture pointer calibration is required",
  );
  return {
    x: pointer.screenX - pointer.clientX,
    y: pointer.screenY - pointer.clientY,
    source: "actual-pointer",
  };
}
async function fixtureProof(page, id, adapter, dir, phase) {
  const prior = (await cli("state", id)).fixture?.instance;
  const navigation = [];
  const saveNavigation = async (stage, details = {}) => {
    navigation.push({ stage, at: new Date().toISOString(), ...details });
    await writeFile(
      path.join(dir, `${phase}-navigation.json`),
      JSON.stringify(navigation, null, 2) + "\n",
    );
  };
  await saveNavigation("before-focus", {
    prior: prior ?? null,
    target: { x: 300, y: 250 },
  });
  await remoteClick(page, adapter, 300, 250);
  const focused = await poll(
    async () => {
      const value = await cli("state", id);
      const a = held(value);
      await saveNavigation("focus-observation", {
        actual: value.actual,
        owner: value.control?.owner ?? null,
      });
      return (
        value.control?.owner === "human" &&
        Math.abs(value.actual.X - 300) <= 2 &&
        Math.abs(value.actual.Y - 250) <= 2 &&
        !a.keys &&
        a.buttons === 0 &&
        value
      );
    },
    { label: "actual content-focus pointer delivery" },
  );
  await saveNavigation("actual-pointer-delivered", {
    actual: focused.actual,
    focusSettleMs: 150,
    typingDelayMs: 16,
  });
  await pause(150);
  await page.keyboard.press("Control+KeyL", { delay: 50 });
  await pause(150);
  await page.keyboard.press("Control+KeyA", { delay: 40 });
  await pause(80);
  await physicalURL(page, "http://127.0.0.1:8081/fixture");
  await pause(80);
  await page.keyboard.press("Enter", { delay: 50 });
  await saveNavigation("typed-explicit-http-fixture-url");
  let s = await poll(
    async () => {
      const v = await cli("state", id);
      return (
        v.fixture?.instance &&
        v.fixture.instance !== prior &&
        v.fixture.rects?.typing &&
        v
      );
    },
    { timeout: 12000, label: "new guest fixture instance" },
  );
  const fixtureInstance = s.fixture.instance;
  // Do not infer a first target from initial window geometry. The safe content
  // point was observed above and in the actual fixed desktop screenshot. Click
  // it again in this fresh fixture, then use its actual screen/client event.
  const calibrationPoint = { x: 300, y: 250 };
  await saveNavigation("fixture-ready-before-calibration", {
    state: safeState(s),
    requestedPoint: calibrationPoint,
  });
  // The prior Home click may have left the pointer at exactly this position.
  // After navigation, move within the fixture first so the viewer and guest
  // observe a fresh pointer transition before the calibration click.
  await pause(250);
  const approach = await point(page, adapter, 292, 242);
  await page.mouse.move(approach.x, approach.y);
  await pause(100);
  await saveNavigation("fixture-calibration-approach", {
    requestedPoint: { x: 292, y: 242 },
    settleMs: 100,
  });
  await remoteClick(page, adapter, calibrationPoint.x, calibrationPoint.y);
  s = await poll(
    async () => {
      const value = await cli("state", id),
        pointer = value.fixture?.pointer;
      await saveNavigation("pointer-calibration-observation", {
        state: safeState(value),
        requestedPoint: calibrationPoint,
      });
      return (
        value.fixture?.instance === fixtureInstance &&
        pointer &&
        Math.abs(pointer.screenX - calibrationPoint.x) <= 2 &&
        Math.abs(pointer.screenY - calibrationPoint.y) <= 2 &&
        value
      );
    },
    { label: "fresh actual fixture pointer calibration" },
  );
  const origin = contentOrigin(s.fixture),
    rect = s.fixture.rects.typing;
  const typingPoint = {
    x: origin.x + rect.x + Math.min(rect.width / 2, 100),
    y: origin.y + rect.y + 20,
  };
  await saveNavigation("calibrated-textarea-target", {
    state: safeState(s),
    origin,
    rect,
    requestedPoint: typingPoint,
  });
  await remoteClick(page, adapter, typingPoint.x, typingPoint.y);
  await page.keyboard.press("Control+KeyA");
  const marker = `e2e-${adapter.endsWith("neko") ? "neko" : "novnc"}-${randomBytes(6).toString("hex")}`;
  await page.keyboard.type(marker, { delay: 8 });
  s = await poll(
    async () => {
      const v = await cli("state", id);
      return v.fixture?.text === marker && v;
    },
    { label: "exact fresh guest text" },
  );
  const clickRect = s.fixture.rects["click-target"];
  assert(
    clickRect && clickRect.height > 0,
    "Fixture click-target rect unavailable",
  );
  const before = s.fixture.counts.click;
  await saveNavigation("calibrated-click-target", {
    state: safeState(s),
    origin,
    rect: clickRect,
    requestedPoint: {
      x: origin.x + clickRect.x + clickRect.width / 2,
      y: origin.y + clickRect.y + clickRect.height / 2,
    },
  });
  await remoteClick(
    page,
    adapter,
    origin.x + clickRect.x + clickRect.width / 2,
    origin.y + clickRect.y + clickRect.height / 2,
  );
  s = await poll(
    async () => {
      const v = await cli("state", id);
      return (
        v.fixture?.text === marker && v.fixture.counts.click === before + 1 && v
      );
    },
    { label: "fresh actual click counter" },
  );
  const native = await cli("capture", id, [], { binary: true });
  await writeFile(path.join(dir, `${phase}-native.png`), native);
  const roi = {
    x: Math.round(origin.x + rect.x + 9),
    y: Math.round(origin.y + rect.y + 8),
    width: Math.min(Math.floor(rect.width - 20), marker.length * 10 + 12),
    height: 24,
  };
  assert(
    roi.x >= 0 &&
      roi.y >= 0 &&
      roi.x + roi.width <= 1280 &&
      roi.y + roi.height <= 720,
    "Marker ROI outside desktop",
  );
  const agreement = await poll(
    async () => {
      const m = await pixelAgreement(page, adapter, native, roi);
      return (
        m.inkPixels >= 40 &&
        m.matchedInkFraction >= 0.8 &&
        m.meanInkError < 28 &&
        m
      );
    },
    {
      timeout: 5000,
      interval: 100,
      label: "decoded marker/native pixel agreement",
    },
  );
  await writeFile(
    path.join(dir, `${phase}-decoded.png`),
    await decodedPNG(page, adapter),
  );
  return {
    marker,
    instance: s.fixture.instance,
    text: s.fixture.text,
    clickCount: s.fixture.counts.click,
    origin,
    rect,
    roi,
    agreement,
  };
}
async function writeGuestWAV(id, filename, wav) {
  assert(
    wav.length <= 20 * 1024,
    "Synthetic WAV exceeds private CLI write bound",
  );
  return new Promise((resolve, reject) => {
    const child = execFile(
      cliPath,
      ["write", "--socket", socket, "--id", id, "--path", filename],
      { cwd: root, encoding: "utf8", timeout: 10000, maxBuffer: 1 << 20 },
      (error, stdout, stderr) => {
        if (error) {
          reject(
            Error(`Guest WAV write failed: ${redact(stderr || error.message)}`),
          );
          return;
        }
        try {
          resolve(JSON.parse(stdout));
        } catch {
          reject(Error("Invalid guest WAV write response"));
        }
      },
    );
    child.stdin.on("error", () =>
      reject(Error("Guest WAV input pipe unavailable")),
    );
    child.stdin.end(wav);
  });
}
async function audioStats(page, instance) {
  const snapshot = await page.evaluate(async () => {
    const client = window.__sidekicks.client;
    const reports = await window.__sidekicks.stats();
    const inbound = reports.find(
      (r) =>
        r.type === "inbound-rtp" &&
        (r.kind === "audio" || r.mediaType === "audio"),
    );
    const codec = reports.find((r) => r.id === inbound?.codecId);
    const transport =
      reports.find(
        (r) => r.id === inbound?.transportId && r.selectedCandidatePairId,
      ) ||
      reports.find((r) => r.type === "transport" && r.selectedCandidatePairId);
    const pair = reports.find(
      (r) => r.id === transport?.selectedCandidatePairId,
    );
    const remote = reports.find((r) => r.id === pair?.remoteCandidateId),
      local = reports.find((r) => r.id === pair?.localCandidateId);
    return {
      track: client.transport.pc
        .getReceivers()
        .filter((r) => r.track?.kind === "audio")
        .map((r) => ({ kind: r.track.kind, readyState: r.track.readyState })),
      inbound: inbound
        ? {
            packetsReceived: inbound.packetsReceived,
            bytesReceived: inbound.bytesReceived,
            totalSamplesReceived: inbound.totalSamplesReceived,
            totalSamplesDuration: inbound.totalSamplesDuration,
            totalAudioEnergy: inbound.totalAudioEnergy,
            jitterBufferEmittedCount: inbound.jitterBufferEmittedCount,
          }
        : null,
      codec: codec
        ? {
            mimeType: codec.mimeType,
            clockRate: codec.clockRate,
            channels: codec.channels,
          }
        : null,
      pair: pair
        ? {
            state: pair.state,
            remoteAddress: remote?.address || remote?.ip,
            remotePort: remote?.port,
            remoteProtocol: remote?.protocol,
            localProtocol: local?.protocol,
          }
        : null,
    };
  });
  const ports = instance.endpoints
    .filter((p) => p.protocol === "udp")
    .map((p) => p.port);
  assert(
    snapshot.track.some((t) => t.readyState === "live") &&
      snapshot.codec?.mimeType?.toLowerCase() === "audio/opus" &&
      snapshot.codec.clockRate === 48000,
    "No live received Opus track",
  );
  assert(
    snapshot.pair?.state === "succeeded" &&
      snapshot.pair.remoteProtocol?.toLowerCase() === "udp" &&
      snapshot.pair.localProtocol?.toLowerCase() === "udp" &&
      snapshot.pair.remoteAddress === "127.0.0.1" &&
      ports.includes(snapshot.pair.remotePort),
    "Audio did not use the allocated selected UDP pair",
  );
  assert(
    Number.isFinite(snapshot.inbound?.packetsReceived) &&
      snapshot.inbound.packetsReceived > 0,
    "No received audio packets",
  );
  return snapshot;
}
async function audioProof(page, id, adapter, dir, phase, instance) {
  assert(adapter === "desktop-neko", "Only Neko has an audio transport");
  // Exec has its own lease. Explicitly release human ownership first; never
  // preempt or reinterpret the expected human/exec ownership conflict.
  await release(page, id);
  const guestExec = async (argv, { allowNonZero = false } = {}) => {
    const state = await cli("state", id);
    const actual = held(state);
    assert(
      !state.control?.owner && !actual.keys && actual.buttons === 0,
      "Audio exec requires unowned held-empty desktop",
    );
    const result = await cli("exec", id, ["--timeout", "5s", "--", ...argv]);
    assert(
      !result.timedOut && !result.stdoutTruncated && !result.stderrTruncated,
      "Audio guest command timed out or truncated",
    );
    if (!allowNonZero)
      assert(
        result.exitCode === 0,
        `Audio guest prerequisite/playback command failed (exit ${result.exitCode})`,
      );
    return result;
  };
  const availability = await guestExec(["/usr/bin/paplay", "--version"]);
  const packages = await guestExec(
    [
      "/usr/bin/dpkg-query",
      "-W",
      "-f=${binary:Package}\t${Version}\t${db:Status-Status}\n",
      "pulseaudio-utils",
      "gstreamer1.0-plugins-base",
      "gstreamer1.0-plugins-good",
      "gstreamer1.0-plugins-bad",
      "gstreamer1.0-plugins-ugly",
      "gstreamer1.0-pulseaudio",
      "libgstreamer-gl1.0-0",
      "libegl1",
      "libgl1",
    ],
    { allowNonZero: true },
  );
  const tone = makeToneWAV(),
    filename = `audio-${phase}-${randomBytes(6).toString("hex")}.wav`;
  await writeGuestWAV(id, filename, tone.wav);
  const read = await cli("read", id, ["--path", filename], { binary: true });
  assert(read.equals(tone.wav), "Guest WAV file differs from generated source");
  const source = {
    frequencyHz: tone.frequencyHz,
    durationMs: tone.durationMs,
    sampleRate: tone.sampleRate,
    channels: tone.channels,
    amplitude: tone.amplitude,
    bytes: tone.wav.length,
    sha256: createHash("sha256").update(tone.wav).digest("hex"),
    guestPath: "/workspace/" + filename,
    playback:
      "guest paplay -> guest Pulse audio_output null sink -> monitor -> GStreamer Opus -> selected UDP WebRTC -> receiver PCM",
  };
  await writeFile(path.join(dir, `${phase}-audio-source.wav`), tone.wav);
  const initial = await poll(() => audioStats(page, instance), {
    timeout: 15000,
    label: "received Opus audio over UDP",
  });
  let setup;
  const trials = [];
  try {
    setup = await page.evaluate(async () => {
      const client = window.__sidekicks.client;
      client.mute();
      const video = document.querySelector("#media video");
      if (video) video.muted = true;
      const track = client.transport.pc
        .getReceivers()
        .find((r) => r.track?.kind === "audio")?.track;
      if (!track || track.readyState !== "live")
        throw Error("Live remote audio receiver unavailable");
      const ctx = new AudioContext({ sampleRate: 48000 }),
        source = ctx.createMediaStreamSource(new MediaStream([track])),
        analyser = ctx.createAnalyser(),
        silence = ctx.createGain();
      silence.gain.value = 0;
      analyser.fftSize = 4096;
      analyser.smoothingTimeConstant = 0;
      source.connect(analyser);
      analyser.connect(silence);
      silence.connect(ctx.destination);
      await ctx.resume();
      if (ctx.state !== "running")
        throw Error("Receiver AudioContext did not start");
      window.__e2eAudio = {
        ctx,
        source,
        analyser,
        silence,
        start(durationMs) {
          if (durationMs < 200 || durationMs > 2500)
            throw Error("Audio window outside bounded range");
          const readings = [],
            samples = new Float32Array(analyser.fftSize);
          let best = -1,
            bestPCM = [],
            began = performance.now();
          this.result = new Promise((resolve) => {
            const sample = () => {
              analyser.getFloatTimeDomainData(samples);
              let power = 0,
                peak = 0;
              for (const v of samples) {
                power += v * v;
                peak = Math.max(peak, Math.abs(v));
              }
              const rms = Math.sqrt(power / samples.length),
                atMs = performance.now() - began;
              readings.push({ atMs, rms, peakAbs: peak });
              if (rms > best) {
                best = rms;
                bestPCM = Array.from(samples);
              }
              if (atMs >= durationMs || readings.length >= 260)
                resolve({ sampleRate: ctx.sampleRate, readings, bestPCM });
              else setTimeout(sample, 10);
            };
            sample();
          });
        },
      };
      return {
        contextState: ctx.state,
        sampleRate: ctx.sampleRate,
        fftSize: analyser.fftSize,
        receiverTrackKind: track.kind,
        videoMuted: !!video?.muted,
        speakerGain: silence.gain.value,
      };
    });
    assert(
      setup.videoMuted && setup.speakerGain === 0,
      "Physical speaker output was not suppressed",
    );
    await page.evaluate(() => {
      window.__e2eAudio.start(650);
    });
    const baseline = await bounded(
      page.evaluate(() => window.__e2eAudio.result),
      3500,
      "silent receiver baseline",
    );
    await writeFile(
      path.join(dir, `${phase}-audio-baseline.json`),
      JSON.stringify(baseline) + "\n",
    );
    for (let i = 0; i < 3; i++) {
      const before = await audioStats(page, instance);
      await page.evaluate(() => {
        window.__e2eAudio.start(1600);
      });
      await pause(120);
      const played = await guestExec([
        "/usr/bin/paplay",
        "--server=unix:/tmp/pulseaudio.socket",
        "--device=audio_output",
        "/workspace/" + filename,
      ]);
      const capture = await bounded(
        page.evaluate(() => window.__e2eAudio.result),
        4000,
        "decoded receiver tone capture",
      );
      await writeFile(
        path.join(dir, `${phase}-audio-tone-${i + 1}.json`),
        JSON.stringify(capture) + "\n",
      );
      const analysis = verifyTone(baseline, capture),
        after = await audioStats(page, instance);
      assert(
        after.inbound.packetsReceived > before.inbound.packetsReceived &&
          after.inbound.bytesReceived > before.inbound.bytesReceived,
        "No fresh Opus packets during guest tone",
      );
      trials.push({
        trial: i + 1,
        playbackExitCode: played.exitCode,
        analysis,
        before,
        after,
        raw: `${phase}-audio-tone-${i + 1}.json`,
      });
    }
  } finally {
    await page
      .evaluate(async () => {
        const a = window.__e2eAudio;
        if (a) {
          a.source.disconnect();
          a.analyser.disconnect();
          a.silence.disconnect();
          await a.ctx.close();
          delete window.__e2eAudio;
        }
      })
      .catch(() => {});
  }
  await guestExec(["/usr/bin/rm", "--", "/workspace/" + filename]);
  await take(page, id, adapter);
  const proof = {
    source,
    setup,
    initial,
    trials,
    sourceFileRemoved: true,
    paplayVersion: availability.stdout.trim(),
    packageQuery: {
      exitCode: packages.exitCode,
      stdout: packages.stdout,
      stderr: packages.stderr,
    },
    limitations: [
      "Physical listening and host speaker/device behavior are untested; output was suppressed in the task-owned browser",
      "Paplay proves guest Pulse/GStreamer/Opus transport; Firefox-generated audio and arbitrary media codecs are separate",
      "No latency, fidelity, device, WAN, or native-input parity claim",
      "Optional upstream H.265/AV1 and RTMP/AAC broadcast paths may be unavailable when Bad is removed; retained Ugly/H.264 is not exercised by VP8/Opus proof",
    ],
  };
  await writeFile(
    path.join(dir, `${phase}-audio-proof.json`),
    JSON.stringify(proof, null, 2) + "\n",
  );
  return proof;
}

async function connectPage(context, id, adapter, instance, dir, phase) {
  const observed = await inspectedAllocation(id, adapter, instance);
  const connection = await cli("connect", id);
  assert(
    typeof connection.url === "string" &&
      /^http:\/\/127\.0\.0\.1:\d+\/#ticket=[A-Za-z0-9_-]{43}$/.test(
        connection.url,
      ),
    "Invalid private viewer URL",
  );
  secrets.add(connection.url);
  secrets.add(connection.url.split("ticket=")[1]);
  const page = await context.newPage();
  await page.goto(connection.url, {
    waitUntil: "domcontentloaded",
    timeout: 15000,
  });
  await poll(
    async () =>
      await page.evaluate(() => !location.hash && !!window.__sidekicks),
    { label: "ticket consumed and stripped" },
  );
  const initial = await poll(
    async () => {
      const i = await sourceInfo(page, adapter);
      return i?.width === 1280 && i.height === 720 && i;
    },
    { timeout: 20000, label: "fresh decoded desktop" },
  );
  const m = await poll(async () => await media(page, adapter, observed), {
    timeout: 10000,
    label: "selected real media route",
  });
  const visual = await poll(
    async () => {
      const value = await homeVisual(page, adapter);
      return (
        value &&
        value.brightFraction > 0.75 &&
        value.cyanIconPixels >= 800 &&
        value.darkGlyphPixels >= 300 &&
        value
      );
    },
    {
      timeout: 15000,
      interval: 150,
      label: "actual decoded Home panel and launcher icons",
    },
  );
  await writeFile(
    path.join(dir, `${phase}-home-visual.json`),
    JSON.stringify(visual, null, 2) + "\n",
  );
  await writeFile(
    path.join(dir, `${phase}-home-decoded.png`),
    await decodedPNG(page, adapter),
  );
  return {
    page,
    media: m,
    geometry: initial,
    publicURL: new URL(connection.url).origin + "/",
    instance: observed,
    homeVisual: visual,
  };
}
async function run(adapter) {
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const id = `sidekicks-compute-e2e-${adapter}-${randomBytes(6).toString("hex")}`;
  const dir = path.join(root, "artifacts/compute/e2e", `${stamp}-${adapter}`);
  await mkdir(dir, { recursive: true });
  const result = {
    schemaVersion: 1,
    adapter,
    id,
    startedAt: new Date().toISOString(),
    status: "RUNNING",
    checks: [],
    limitations: [
      "Synthetic task-owned Chrome automation, not physical keyboard/phone coverage",
      "Startup Home PNGs require the coordinator's pixel review; decoded dimensions alone do not identify Home",
      "No performance benchmark, application-queue flush, WAN or isolation-escape claim",
    ],
  };
  let server,
    browser,
    context,
    page,
    protectedBefore,
    created = false,
    creationAttempted = false,
    failed = false;
  const save = async () =>
    writeFile(
      path.join(dir, "result.json"),
      JSON.stringify(result, null, 2) + "\n",
    );
  const pass = async (name, details = {}) => {
    checkCancellation();
    result.checks.push({
      name,
      status: "PASS",
      at: new Date().toISOString(),
      ...details,
    });
    await save();
  };
  result.lifecycleTrace = [];
  lifecycleTraces.set(id, {
    entries: result.lifecycleTrace,
    persist: save,
    persistenceFailed: false,
  });
  await save();
  try {
    protectedBefore = await protectedContainers();
    await writeFile(
      path.join(dir, "protected-before.json"),
      JSON.stringify(protectedBefore, null, 2) + "\n",
    );
    assert(
      !protectedBefore.some(
        (value) => value.name === "/" + id || value.name === id,
      ),
      "Generated E2E container name already exists; no create/lifecycle operation issued",
    );
    creationAttempted = true;
    const instance = await cli("create", id, [
      "--adapter",
      adapter,
      "--image",
      imageOverride || `sidekicks-compute/${adapter}:clean`,
      "--network",
      "egress",
      "--publish-viewer",
      "--memory-mib",
      "2048",
    ]);
    result.instance = instance;
    assert(
      instance.id === id &&
        instance.adapter === adapter &&
        instance.runtime === "docker",
      "Unexpected created instance identity",
    );
    assert(
      !imageOverride || instance.image === imageOverride,
      "Created image differs from requested immutable ID",
    );
    created = true;
    result.requestedImage =
      imageOverride || `sidekicks-compute/${adapter}:clean`;
    result.imageID = instance.image;
    result.audioEnabled = audioEnabled;
    result.creationAcknowledged = true;
    await waitReady(id);
    await pass("owned isolated desktop ready", {
      image: instance.image,
      container: id,
    });
    server = await chromium.launchServer({
      channel: "chrome",
      headless: true,
      chromiumSandbox: true,
      args: ["--autoplay-policy=no-user-gesture-required"],
      timeout: 15000,
    });
    secrets.add(server.wsEndpoint());
    result.ownedChromePID = server.process().pid;
    browser = await chromium.connect(server.wsEndpoint());
    context = await browser.newContext({
      viewport: { width: 1440, height: 920 },
      deviceScaleFactor: 1,
    });
    let connected = await connectPage(
      context,
      id,
      adapter,
      instance,
      dir,
      "before",
    );
    page = connected.page;
    await pass("actual decoded startup desktop and media", {
      viewer: connected.publicURL,
      media: connected.media,
      screenshot: "before-home-decoded.png",
      homeVisual: connected.homeVisual,
      inspectedEndpoints: connected.instance.endpoints,
    });
    const beforeAgent = await cli("state", id);
    const initialInput = held(beforeAgent);
    assert(
      !beforeAgent.control?.owner &&
        !initialInput.keys &&
        initialInput.buttons === 0,
      "Agent injection proof requires an initially unowned held-empty desktop",
    );
    const agent = await cli("control", id, [
      "--request",
      JSON.stringify({
        action: "acquire",
        owner: "agent",
        session: `e2e-agent-${randomBytes(4).toString("hex")}`,
        leaseMs: 60000,
      }),
    ]);
    secrets.add(agent.token);
    await cli("input", id, [
      "--request",
      JSON.stringify({
        session: agent.session,
        token: agent.token,
        events: [{ type: "key", key: "Shift", action: "down" }],
      }),
    ]);
    const agentHeld = await poll(
      async () => {
        const state = await cli("state", id),
          actual = held(state);
        return (
          state.control?.owner === "agent" &&
          state.control.session === agent.session &&
          actual.keys &&
          (actual.buttons & 1) !== 0 &&
          (actual.buttons & 0x1f00) === 0 &&
          state
        );
      },
      { timeout: 5000, label: "actual agent-injected Shift held in X11" },
    );
    await pass("real agent Shift input acknowledged and observed in X11", {
      before: safeState(beforeAgent),
      held: safeState(agentHeld),
    });
    const afterTake = await take(page, id, adapter);
    await pass(
      "human takeover acknowledges actual release of agent-held Shift",
      {
        heldBefore: safeState(agentHeld),
        firstObservedAfterAcknowledgment: safeState(afterTake),
      },
    );
    if (adapter === "desktop-novnc") {
      const original = await page.evaluate(() => {
        window.__e2eOriginalRFB = window.__sidekicks.rfb;
        return {
          epoch: window.__sidekicks.epoch,
          connected:
            window.__sidekicks.rfb?._rfbConnectionState === "connected",
        };
      });
      assert(
        Number.isSafeInteger(original.epoch) &&
          original.epoch > 0 &&
          original.connected,
        "First Take did not establish a connected epoch-bound RFB stream",
      );
      const responsePromise = page.waitForResponse(
        (response) => {
          if (
            !response.url().endsWith("/control") ||
            response.request().method() !== "POST"
          )
            return false;
          try {
            return response.request().postDataJSON().action === "acquire";
          } catch {
            return false;
          }
        },
        { timeout: 10000 },
      );
      await page.locator("#take").click();
      const response = await responsePromise;
      assert(response.status() === 200, "Repeated Take was rejected");
      const acknowledged = await response.json();
      assert(
        acknowledged.epoch === original.epoch && acknowledged.owned,
        "Repeated Take changed the active lease epoch",
      );
      await poll(
        () =>
          page.evaluate(
            (expected) =>
              window.__sidekicks.controlling &&
              window.__sidekicks.epoch === expected &&
              window.__sidekicks.rfb === window.__e2eOriginalRFB &&
              window.__sidekicks.rfb?._rfbConnectionState === "connected",
            original.epoch,
          ),
        { label: "repeated Take retains same connected RFB object" },
      );
      await page.evaluate(() => {
        delete window.__e2eOriginalRFB;
      });
      await pass("repeated Take retains same connected RFB stream and epoch", {
        epoch: original.epoch,
        sameStream: true,
      });
    }
    const transportInput = await page.evaluate(
      (kind) => ({
        mode: window.__sidekicks.inputMode,
        native: window.__sidekicks.nativeInputSupported,
        rfbReadOnly:
          kind === "desktop-novnc" ? window.__sidekicks.rfb.viewOnly : null,
        upstreamNativeController:
          kind === "desktop-neko"
            ? window.__sidekicks.client.controlling
            : null,
      }),
      adapter,
    );
    assert(
      transportInput.mode === "gated" && transportInput.native === false,
      "Viewer is not using owned Go input",
    );
    if (adapter === "desktop-novnc")
      assert(
        transportInput.rfbReadOnly === true,
        "RFB input unexpectedly enabled",
      );
    if (adapter === "desktop-neko") {
      assert(
        transportInput.upstreamNativeController === false,
        "Neko native input unexpectedly granted",
      );
      const nativeStatus = await page.evaluate(async () => {
        const response = await fetch("/control", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            action: "acquire",
            mode: "native",
            leaseMs: 30000,
          }),
        });
        return response.status;
      });
      assert(
        [409, 422, 503].includes(nativeStatus),
        "Unsupported native ownership was not rejected",
      );
      transportInput.nativeAcquireStatus = nativeStatus;
    }
    await pass(
      "protocol media receive-only and Go gated input selected",
      transportInput,
    );
    let rejected;
    try {
      await cli("input", id, [
        "--request",
        JSON.stringify({
          session: agent.session,
          token: agent.token,
          events: [{ type: "text", text: "stale" }],
        }),
      ]);
    } catch (e) {
      rejected = /http409/.test(e.message);
    }
    assert(rejected, "Preempted agent input was not rejected409");
    await pass("human takeover rejects previously valid agent input", {
      rejectedStatus: 409,
    });
    const beforeProof = await fixtureProof(page, id, adapter, dir, "before");
    await pass(
      "actual viewer typing click and decoded marker agreement",
      beforeProof,
    );
    if (audioEnabled)
      await pass(
        "received Opus synthetic audio before restart",
        await audioProof(page, id, adapter, dir, "before", connected.instance),
      );
    const target = await point(
      page,
      adapter,
      beforeProof.origin.x + beforeProof.rect.x + 80,
      beforeProof.origin.y + beforeProof.rect.y + 22,
    );
    const beforeHold = await cli("state", id);
    assert(
      !held(beforeHold).keys && held(beforeHold).buttons === 0,
      "Unexpected input already held before safety setup",
    );
    await page.mouse.move(target.x, target.y);
    await page.mouse.down();
    await poll(
      async () => {
        const s = await cli("state", id);
        return (held(s).buttons & 256) !== 0 && !held(s).keys;
      },
      { label: "actual button down before Shift" },
    );
    await page.keyboard.down("Shift");
    const holdTrace = [];
    result.holdSetup = holdTrace;
    const heldBefore = await poll(
      async () => {
        const s = await cli("state", id);
        const h = held(s);
        holdTrace.push({
          at: new Date().toISOString(),
          actual: s.actual,
          fixtureHeldKeys: s.fixture?.heldKeys ?? null,
          owner: s.control?.owner ?? null,
        });
        await save();
        return (
          h.keys &&
          (h.buttons & 256) !== 0 &&
          s.fixture?.heldKeys?.some(
            (code) => code === "ShiftLeft" || code === "ShiftRight",
          ) &&
          s
        );
      },
      { label: "actual Shift plus left held" },
    );
    const released = await release(page, id);
    await pass(
      "UI release clears proven actual Shift and button before local cleanup",
      { before: safeState(heldBefore), after: safeState(released) },
    );
    await page.mouse.up();
    await page.keyboard.up("Shift");
    if (adapter === "desktop-novnc") {
      await pass(
        "observer-first socket loss clears ownership and actual held input",
        await observerDisconnectProof(browser, id, instance, beforeProof),
      );
    }
    await page.locator("#fullscreen").click();
    await poll(() => page.evaluate(() => !!document.fullscreenElement), {
      label: "viewer fullscreen entry",
    });
    await page.keyboard.press("Escape");
    await poll(() => page.evaluate(() => !document.fullscreenElement), {
      label: "viewer fullscreen Escape",
    });
    await pass("viewer fullscreen entry and Escape reversible", {
      physicalReservedKeys: "not tested",
    });
    await bounded(context.close(), 5000, "owned context close");
    context = null;
    page = null;
    await cli("stop", id);
    const restarted = await cli("start", id);
    assert(
      restarted.id === instance.id && restarted.image === instance.image,
      "Normal start replaced instance identity/image",
    );
    await waitReady(id);
    await pass("one supported normal stop/start", { instance: restarted });
    context = await browser.newContext({
      viewport: { width: 1440, height: 920 },
      deviceScaleFactor: 1,
    });
    connected = await connectPage(
      context,
      id,
      adapter,
      restarted,
      dir,
      "after",
    );
    page = connected.page;
    await pass("postrestart fresh decoded desktop and media", {
      viewer: connected.publicURL,
      media: connected.media,
      screenshot: "after-home-decoded.png",
      homeVisual: connected.homeVisual,
      inspectedEndpoints: connected.instance.endpoints,
    });
    await take(page, id, adapter);
    await pass(
      "postrestart actual viewer input and fresh decoded capture",
      await fixtureProof(page, id, adapter, dir, "after"),
    );
    if (audioEnabled)
      await pass(
        "received Opus synthetic audio after restart",
        await audioProof(page, id, adapter, dir, "after", connected.instance),
      );
    const finalState = await release(page, id);
    const finalMedia = await media(page, adapter, connected.instance);
    if (adapter === "desktop-neko")
      assert(
        finalMedia.framesDecoded > connected.media.framesDecoded,
        "Decoded frame count did not increase with fresh input",
      );
    await pass("final unowned actual held empty and continuing stream", {
      state: safeState(finalState),
      media: finalMedia,
    });
    checkCancellation();
    result.status = "PASS";
  } catch (e) {
    failed = true;
    result.status = "FAIL";
    result.failure = { message: redact(e.message), stack: redact(e.stack) };
    if (page) {
      try {
        await bounded(
          page.screenshot({
            path: path.join(dir, "failure-viewer.png"),
            timeout: 3000,
          }),
          4000,
          "failure screenshot",
        );
      } catch {}
    }
  } finally {
    result.functionalStatus = result.status;
    const cleanup = [];
    if (page) {
      try {
        await bounded(
          page.evaluate(() => window.__sidekicks?.release()),
          4000,
          "own release",
        );
      } catch (e) {
        cleanup.push(redact(e.message));
      }
      try {
        await page.mouse.up();
        await page.keyboard.up("Shift");
      } catch {}
    }
    if (context) {
      try {
        await bounded(context.close(), 5000, "owned context close");
      } catch (e) {
        cleanup.push(redact(e.message));
      }
    }
    // This browser is a client connected to our BrowserServer. Disconnect it
    // before waiting for the server to close; an open client can keep its
    // WebSocket shutdown pending after the Chrome process itself has exited.
    if (browser?.isConnected()) {
      try {
        await bounded(browser.close(), 5000, "owned browser client close");
      } catch (e) {
        cleanup.push(redact(e.message));
      }
    }
    if (server) {
      try {
        result.chromeShutdown = await cleanupOwnedBrowserServer(server);
      } catch (e) {
        result.chromeShutdown = {
          pid: result.ownedChromePID ?? null,
          error: redact(e.message),
          cleanupConfirmed: false,
        };
        cleanup.push(redact(e.message));
      }
    }
    const ownedCleanup = await cleanupAcknowledged(id, created);
    result.cleanupStopped = ownedCleanup.stopped ?? false;
    result.destroyed = ownedCleanup.destroyed ?? false;
    result.cleanupSkippedUnconfirmedCreation =
      !!ownedCleanup.skippedUnconfirmedCreation;
    if (creationAttempted && !created)
      result.creationUncertainty =
        "No verified successful create acknowledgment. The harness issued no stop/destroy by name; runtime ProvisionError immutable-ID cleanup status is authoritative. Orphan state remains unconfirmed by this harness.";
    cleanup.push(...ownedCleanup.errors);
    // Distinguish confirmed owned-resource cleanup from the later strict
    // protection audit. Concurrent changes must still fail the overall gate.
    result.ownedResourcesCleaned =
      result.cleanupStopped && result.destroyed && cleanup.length === 0;
    try {
      if (protectedBefore) {
        const after = await protectedContainers();
        await writeFile(
          path.join(dir, "protected-after.json"),
          JSON.stringify(after, null, 2) + "\n",
        );
        const beforeByID = new Map(
          protectedBefore.map((value) => [value.id, value]),
        );
        const afterByID = new Map(after.map((value) => [value.id, value]));
        const changes = [];
        for (const [id, before] of beforeByID) {
          const current = afterByID.get(id);
          if (JSON.stringify(before) !== JSON.stringify(current))
            changes.push({
              id,
              name: before.name,
              before,
              after: current ?? null,
              kind:
                current && current.restartCount > before.restartCount
                  ? "protected-container-restart-observed"
                  : "protected-resource-change",
            });
        }
        for (const [id, current] of afterByID)
          if (!beforeByID.has(id))
            changes.push({
              id,
              name: current.name,
              before: null,
              after: current,
              kind: "additional-container-resource",
            });
        result.protectedAudit = {
          status: changes.length ? "FAIL" : "PASS",
          scope:
            "All pre-existing Docker containers, including stopped and other-compute resources; no prefix exclusions",
          changes,
          attribution:
            "Harness lifecycle commands target only its acknowledged generated compute ID. This audit observes direct and concurrent changes; it cannot establish causation or exclude indirect effects such as resource pressure.",
        };
        await writeFile(
          path.join(dir, "protected-diff.json"),
          JSON.stringify(result.protectedAudit, null, 2) + "\n",
        );
        assert(
          JSON.stringify(after) === JSON.stringify(protectedBefore),
          "Protected Docker containers changed",
        );
        result.protectedContainersUnchanged = true;
      }
    } catch (e) {
      cleanup.push(redact(e.message));
    }
    if (lifecycleTraces.get(id)?.persistenceFailed)
      cleanup.push("Lifecycle trace persistence failed");
    result.cleanup = cleanup;
    if (cleanup.length) {
      result.status = "FAIL";
      failed = true;
    }
    result.finishedAt = new Date().toISOString();
    await save();
  }
  console.log(
    JSON.stringify({
      adapter,
      status: result.status,
      checks: result.checks.length,
      artifact: path.relative(root, dir),
    }),
  );
  lifecycleTraces.delete(id);
  return !failed;
}
const success = await runSequential(targets, run, () => cancellationRequested);
if (!success) process.exitCode = 1;
