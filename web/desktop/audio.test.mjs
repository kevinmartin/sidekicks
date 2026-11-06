import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { EventEmitter } from "node:events";
import {
  collectOwnedChild,
  inspectOwnedProcessGroup,
  cleanupOwnedBrowserServer,
} from "../../test/e2e/owned-child.mjs";
import {
  makeToneWAV,
  analyzePCM,
  verifyTone,
  validateImageSelection,
} from "../../test/e2e/audio-analysis.mjs";
import {
  parseComparison,
  validateChildResult,
} from "../../test/e2e/neko-audio.mjs";

function fakeChild() {
  const child = new EventEmitter();
  child.stdout = new EventEmitter();
  child.stderr = new EventEmitter();
  child.signals = [];
  child.kill = (signal) => child.signals.push(signal);
  return child;
}
test("owned process group is accepted only on explicit ESRCH", () => {
  const gone = (pid, signal) => {
    assert.equal(pid, -123);
    assert.equal(signal, 0);
    const error = Error("gone");
    error.code = "ESRCH";
    throw error;
  };
  const denied = (pid, signal) => {
    assert.equal(pid, -123);
    assert.equal(signal, 0);
    const error = Error("denied");
    error.code = "EPERM";
    throw error;
  };
  assert.equal(inspectOwnedProcessGroup(123, "darwin", gone).absent, true);
  assert.equal(inspectOwnedProcessGroup(123, "darwin", denied).absent, false);
  assert.equal(
    inspectOwnedProcessGroup(123, "darwin", (pid, signal) => {
      assert.equal(pid, -123);
      assert.equal(signal, 0);
    }).absent,
    false,
  );
  assert.equal(inspectOwnedProcessGroup(0, "darwin", gone).absent, false);
  assert.equal(inspectOwnedProcessGroup(123, "win32", gone).absent, false);
});
function fakeBrowserServer({ exits = true, resolves = true } = {}) {
  const child = new EventEmitter();
  child.pid = 123;
  child.exitCode = null;
  child.signalCode = null;
  let finish;
  let killCalls = 0;
  const promise = new Promise((resolve) => (finish = resolve));
  const pipe = {
    destroyed: false,
    destroy() {
      this.destroyed = true;
      if (resolves) finish();
    },
  };
  child.stdio = [null, pipe];
  return {
    child,
    pipe,
    get killCalls() {
      return killCalls;
    },
    process: () => child,
    kill: () => {
      killCalls++;
      if (exits) {
        child.signalCode = "SIGKILL";
        child.emit("exit", null, "SIGKILL");
      }
      return promise;
    },
  };
}
test("browser cleanup closes owned pipes only after exit and empty process group", async () => {
  const server = fakeBrowserServer();
  let probed = false;
  const result = await cleanupOwnedBrowserServer(server, {
    exitMs: 100,
    groupMs: 100,
    cleanupMs: 100,
    probe: (pid) => {
      assert.equal(pid, 123);
      assert.equal(server.child.signalCode, "SIGKILL");
      assert.equal(server.pipe.destroyed, false);
      probed = true;
      return { absent: true };
    },
  });
  assert.equal(probed, true);
  assert.equal(server.killCalls, 1);
  assert.equal(server.pipe.destroyed, true);
  assert.equal(result.playwrightCleanupCompleted, true);
  assert.equal(result.processGroup.absent, true);
});
test("browser cleanup fails closed without leader exit or with a live process group", async () => {
  const noExit = fakeBrowserServer({ exits: false });
  await assert.rejects(
    cleanupOwnedBrowserServer(noExit, { exitMs: 10 }),
    /exit timed out/,
  );
  assert.equal(noExit.pipe.destroyed, false);
  const liveGroup = fakeBrowserServer();
  await assert.rejects(
    cleanupOwnedBrowserServer(liveGroup, {
      exitMs: 50,
      groupMs: 1,
      cleanupMs: 10,
      probe: () => ({
        absent: false,
        error: "owned process group still exists",
      }),
    }),
    /group remained/,
  );
  assert.equal(liveGroup.pipe.destroyed, false);
});
test("browser cleanup cannot pass if Playwright cleanup does not finish", async () => {
  const server = fakeBrowserServer({ resolves: false });
  await assert.rejects(
    cleanupOwnedBrowserServer(server, {
      exitMs: 50,
      groupMs: 50,
      cleanupMs: 10,
      probe: () => ({ absent: true }),
    }),
    /server cleanup timed out/,
  );
  assert.equal(server.pipe.destroyed, true);
});
test("transient EPERM is never treated as absence but may be retried", async () => {
  const server = fakeBrowserServer();
  let calls = 0;
  const result = await cleanupOwnedBrowserServer(server, {
    exitMs: 50,
    groupMs: 200,
    cleanupMs: 50,
    probe: () =>
      ++calls === 1
        ? { absent: false, error: "owned process group check failed: EPERM" }
        : { absent: true },
  });
  assert.equal(calls, 2);
  assert.equal(result.processGroupObservations[0].absent, false);
  assert.equal(result.processGroup.absent, true);
});
test("browser cleanup fails on Playwright kill rejection or an uninspectable group", async () => {
  const rejected = fakeBrowserServer();
  rejected.kill = () => {
    rejected.child.exitCode = 0;
    rejected.child.emit("exit", 0, null);
    return Promise.reject(Error("kill failed"));
  };
  await assert.rejects(
    cleanupOwnedBrowserServer(rejected, {
      exitMs: 50,
      groupMs: 50,
      cleanupMs: 50,
      probe: () => ({ absent: true }),
    }),
    /kill failed/,
  );
  const denied = fakeBrowserServer();
  await assert.rejects(
    cleanupOwnedBrowserServer(denied, {
      exitMs: 50,
      groupMs: 50,
      cleanupMs: 50,
      probe: () => ({
        absent: false,
        error: "owned process group check failed: EPERM",
      }),
    }),
    /EPERM/,
  );
  assert.equal(denied.pipe.destroyed, false);
});
test("owned child completes normally with bounded output", async () => {
  const child = fakeChild();
  const result = collectOwnedChild(child, { deadlineMs: 1000 });
  child.stdout.emit("data", Buffer.from('{"status":"PASS"}\n'));
  child.emit("close", 0, null);
  assert.equal((await result).summary.status, "PASS");
  assert.deepEqual(child.signals, []);
});
test("deadline requests cooperative cleanup then forces only owned child and reports uncertainty", async () => {
  const child = fakeChild();
  const result = collectOwnedChild(child, {
    deadlineMs: 10,
    graceMs: 10,
    killWaitMs: 10,
  });
  await assert.rejects(
    result,
    /deadline.*termination and cleanup are unconfirmed/,
  );
  assert.deepEqual(child.signals, ["SIGTERM", "SIGKILL"]);
});
test("deadline remains a failure even if child subsequently closes", async () => {
  const child = fakeChild();
  const result = collectOwnedChild(child, {
    deadlineMs: 10,
    graceMs: 100,
    killWaitMs: 10,
  });
  setTimeout(() => child.emit("close", 0, null), 30);
  await assert.rejects(result, /deadline.*cleanup is unconfirmed/);
  assert.deepEqual(child.signals, ["SIGTERM"]);
});
test("kill errors during cancellation do not suppress forced termination", async () => {
  const child = fakeChild();
  child.kill = (signal) => {
    child.signals.push(signal);
    child.emit("error", Error("kill failed"));
  };
  const result = collectOwnedChild(child, {
    deadlineMs: 10,
    graceMs: 10,
    killWaitMs: 10,
  });
  await assert.rejects(
    result,
    /deadline.*termination and cleanup are unconfirmed/,
  );
  assert.deepEqual(child.signals, ["SIGTERM", "SIGKILL"]);
});
test("oversized child output requests cleanup and cannot pass", async () => {
  const child = fakeChild();
  const result = collectOwnedChild(child, { deadlineMs: 1000, graceMs: 100 });
  child.stdout.emit("data", Buffer.alloc((1 << 20) + 1));
  child.emit("close", 0, null);
  await assert.rejects(result, /output bound.*cleanup is unconfirmed/);
  assert.deepEqual(child.signals, ["SIGTERM"]);
});
function sine(frequency = 440) {
  return Array.from(
    { length: 4096 },
    (_, i) => 0.06 * Math.sin((2 * Math.PI * frequency * i) / 48000),
  );
}
const baseline = {
  readings: [
    { atMs: 0, rms: 0 },
    { atMs: 600, rms: 0 },
  ],
};
const capture = (samples) => ({
  sampleRate: 48000,
  bestPCM: samples,
  readings: [
    { atMs: 0, rms: 0 },
    { atMs: 100, rms: 0.04 },
    { atMs: 150, rms: 0.04 },
    { atMs: 250, rms: 0.04 },
  ],
});
test("source WAV is deterministic PCM16 and fits private file RPC", () => {
  const tone = makeToneWAV(),
    other = makeToneWAV();
  assert.ok(tone.wav.equals(other.wav));
  assert.ok(tone.wav.length < 20 * 1024);
  assert.equal(tone.wav.toString("ascii", 0, 4), "RIFF");
  assert.equal(tone.wav.readUInt32LE(24), 16000);
  assert.equal(tone.wav.readUInt16LE(22), 1);
  assert.equal(tone.wav.readUInt16LE(34), 16);
  assert.equal(tone.wav.readUInt32LE(40), 16000);
});
test("independent PCM analysis establishes a sustained 440Hz waveform", () => {
  const analysis = analyzePCM(sine(), 48000);
  assert.ok(Math.abs(analysis.dominantHz - 440) <= 2);
  assert.ok(analysis.rms > 0.04 && analysis.rms < 0.045);
  assert.ok(analysis.dominantToTotalRMS > 0.9);
  assert.equal(verifyTone(baseline, capture(sine())).energeticReadings, 3);
});
test("silence, wrong-frequency sound and ambient noise cannot pass as tone", () => {
  assert.throws(() => verifyTone(baseline, capture(Array(4096).fill(0))));
  assert.throws(() => verifyTone(baseline, capture(sine(880))));
  let seed = 37;
  const noise = Array.from({ length: 4096 }, () => {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return (seed / 2 ** 32 - 0.5) * 0.1;
  });
  assert.throws(() => verifyTone(baseline, capture(noise)));
  assert.throws(
    () =>
      verifyTone(
        {
          readings: [
            { atMs: 0, rms: 0.01 },
            { atMs: 10, rms: 0.01 },
          ],
        },
        capture(sine()),
      ),
    /baseline was not quiet/,
  );
});
test("PCM and image selection fail closed on invalid bounds and mutable tags", () => {
  for (const data of [
    [],
    Array(512).fill(NaN),
    Array(16385).fill(0),
    Array(512).fill(2),
  ])
    assert.throws(() => analyzePCM(data, 48000));
  const id = "sha256:" + "a".repeat(64);
  assert.equal(validateImageSelection(id, "desktop-neko"), id);
  for (const value of [
    undefined,
    "sidekicks-compute/desktop-neko:clean",
    "sha256:" + "A".repeat(64),
    "sha256:short",
  ])
    assert.throws(() => validateImageSelection(value, "desktop-neko"));
  assert.throws(() => validateImageSelection(id, "all"));
  for (const readings of [
    [],
    [
      { atMs: 0, rms: 0 },
      { atMs: 20, rms: NaN },
    ],
    [
      { atMs: 100, rms: 0 },
      { atMs: 0, rms: 0 },
    ],
  ])
    assert.throws(
      () => verifyTone({ readings }, capture(sine())),
      /observation window/,
    );
});
test("comparison requires two distinct immutable images and explicit execution", () => {
  const baseline = "sha256:" + "a".repeat(64),
    candidate = "sha256:" + "b".repeat(64);
  const argv = ["--execute", "--baseline", baseline, "--candidate", candidate];
  assert.deepEqual(parseComparison(argv, "1"), { baseline, candidate });
  for (const args of [
    argv.slice(1),
    ["--execute", "--baseline", baseline],
    [...argv, "--candidate", candidate],
    [...argv, "--pull"],
    ["--execute", "--baseline", baseline, "--candidate", baseline],
  ])
    assert.throws(() => parseComparison(args, "1"));
  assert.throws(() => parseComparison(argv, "0"));
});
test("comparison keeps strict environment failure distinct from waveform functional proof", () => {
  const image = "sha256:" + "a".repeat(64),
    summary = {
      adapter: "desktop-neko",
      artifact: "artifacts/compute/e2e/2026-10-30T00-00-00-000Z-desktop-neko",
    };
  const result = {
    adapter: "desktop-neko",
    imageID: image,
    requestedImage: image,
    audioEnabled: true,
    status: "FAIL",
    functionalStatus: "PASS",
    destroyed: true,
    ownedResourcesCleaned: true,
    cleanup: ["Protected Docker containers changed"],
    protectedAudit: { status: "FAIL" },
    checks: ["before", "after"].map((phase) => ({
      name: `received Opus synthetic audio ${phase} restart`,
      status: "PASS",
      trials: [{}, {}, {}],
    })),
  };
  const validated = validateChildResult(summary, result, image);
  assert.equal(validated.status, "FAIL");
  assert.equal(validated.functionalStatus, "PASS");
  assert.equal(validated.audioPass, true);
  assert.equal(validated.cleaned, true);
  assert.equal(validated.protectedAudit, "FAIL");
  assert.throws(() =>
    validateChildResult(
      { ...summary, artifact: "../../elsewhere" },
      result,
      image,
    ),
  );
  assert.throws(() =>
    validateChildResult(
      summary,
      { ...result, imageID: "sha256:" + "b".repeat(64) },
      image,
    ),
  );
  assert.equal(
    validateChildResult(summary, { ...result, checks: [] }, image).audioPass,
    false,
  );
  assert.equal(
    validateChildResult(
      summary,
      { ...result, ownedResourcesCleaned: false },
      image,
    ).cleaned,
    false,
  );
});
test("audio command preflight cannot execute while a human lease remains", async () => {
  const source = fs.readFileSync(
    new URL("../../test/e2e/desktop.mjs", import.meta.url),
    "utf8",
  );
  const held = source.slice(
    source.indexOf("function held(state)"),
    source.indexOf("function safeState(state)"),
  );
  const audio = source.slice(
    source.indexOf("async function audioProof("),
    source.indexOf("async function connectPage("),
  );
  const calls = [];
  const context = {
    assert: (ok, message) => assert.ok(ok, message),
    release: async () => {},
    cli: async (command) => {
      calls.push(command);
      return {
        control: { owner: "human" },
        actual: { Keys: Array(32).fill(0), Buttons: 0 },
      };
    },
  };
  vm.createContext(context);
  vm.runInContext(held + audio, context);
  await assert.rejects(
    context.audioProof({}, "owned", "desktop-neko", "/mock", "before", {}),
    /requires unowned held-empty/,
  );
  assert.deepEqual(calls, ["state"]);
});
