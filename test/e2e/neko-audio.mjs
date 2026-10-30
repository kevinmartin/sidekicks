// Explicit exact-image comparison. The child desktop suite owns all workloads.
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { validateImageSelection } from "./audio-analysis.mjs";
import { collectOwnedChild } from "./owned-child.mjs";

const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);

export function parseComparison(args, enabled) {
  if (enabled !== "1" || !args.includes("--execute"))
    throw Error(
      "Comparison requires --execute and SIDEKICKS_E2E=1 after image handoff",
    );
  const parsed = {};
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--execute") {
      if (parsed.execute) throw Error("Duplicate execution flag");
      parsed.execute = true;
    } else if (args[i] === "--baseline" || args[i] === "--candidate") {
      const key = args[i].slice(2);
      if (parsed[key]) throw Error("Duplicate image selection");
      parsed[key] = validateImageSelection(args[++i], "desktop-neko");
    } else throw Error("Unknown comparison argument");
  }
  validateImageSelection(parsed.baseline, "desktop-neko");
  validateImageSelection(parsed.candidate, "desktop-neko");
  if (parsed.baseline === parsed.candidate)
    throw Error("Baseline and candidate must be distinct immutable images");
  return { baseline: parsed.baseline, candidate: parsed.candidate };
}

export function validateChildResult(summary, result, expectedImage) {
  validateImageSelection(expectedImage, "desktop-neko");
  if (
    summary?.adapter !== "desktop-neko" ||
    !/^artifacts\/compute\/e2e\/[A-Za-z0-9-]+-desktop-neko$/.test(
      summary.artifact || "",
    ) ||
    result?.adapter !== "desktop-neko" ||
    result.imageID !== expectedImage ||
    result.requestedImage !== expectedImage ||
    result.audioEnabled !== true ||
    !["PASS", "FAIL"].includes(result.status) ||
    !["PASS", "FAIL"].includes(result.functionalStatus)
  )
    throw Error(
      "Child result does not establish the exact requested audio workload",
    );
  const phases = {};
  for (const phase of ["before", "after"]) {
    const check = result.checks?.find(
      (entry) =>
        entry.name === `received Opus synthetic audio ${phase} restart` &&
        entry.status === "PASS",
    );
    phases[phase] = check
      ? {
          proof: `${phase}-audio-proof.json`,
          sourceSHA256: check.source?.sha256,
          trials: check.trials?.length || 0,
          selectedPair: check.initial?.pair,
        }
      : null;
  }
  return {
    requestedImage: expectedImage,
    imageID: result.imageID,
    status: result.status,
    functionalStatus: result.functionalStatus,
    audioPhases: phases,
    audioPass: Object.values(phases).every(Boolean),
    cleaned: result.ownedResourcesCleaned === true,
    protectedAudit: result.protectedAudit?.status || "UNCONFIRMED",
    artifact: summary.artifact,
  };
}

function runDesktop(image) {
  // Fixed Node entrypoint/argv; no shell, image pull, retag or existing ID.
  const child = spawn(
    process.execPath,
    [
      path.join(root, "test/e2e/desktop.mjs"),
      "--execute",
      "--adapter",
      "desktop-neko",
      "--image",
      image,
      "--audio",
    ],
    { cwd: root, env: process.env, stdio: ["ignore", "pipe", "pipe"] },
  );
  return collectOwnedChild(child);
}

async function main() {
  const selected = parseComparison(
    process.argv.slice(2),
    process.env.SIDEKICKS_E2E,
  );
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const dir = path.join(root, "artifacts/compute/audio", stamp);
  await mkdir(dir, { recursive: true });
  const result = {
    schemaVersion: 1,
    startedAt: new Date().toISOString(),
    status: "RUNNING",
    ...selected,
    runs: [],
    limitations: [
      "Synthetic guest paplay through Pulse/GStreamer/Opus; not Firefox-generated audio or physical listening",
      "No latency/performance/fidelity/host-device/WAN claim; browser speaker output stays muted at zero gain",
      "Removing Bad loses optional upstream H.265/AV1 encoders and RTMP/AAC broadcasting; retained Ugly/H.264 is not covered by VP8/Opus",
      "App lifecycle is a separate real-guest test of a one-layer derivative of each exact image, not established by this media suite",
      "Strict protection audit includes concurrent container changes; it cannot establish causation or exclude indirect effects",
    ],
  };
  const save = () =>
    writeFile(
      path.join(dir, "result.json"),
      JSON.stringify(result, null, 2) + "\n",
    );
  await save();
  try {
    for (const label of ["baseline", "candidate"]) {
      const child = await runDesktop(selected[label]);
      // Validate path before reading any child-selected filesystem location.
      if (
        !/^artifacts\/compute\/e2e\/[A-Za-z0-9-]+-desktop-neko$/.test(
          child.summary?.artifact || "",
        )
      )
        throw Error("Invalid child artifact path");
      const raw = JSON.parse(
        await readFile(
          path.join(root, child.summary.artifact, "result.json"),
          "utf8",
        ),
      );
      const run = validateChildResult(child.summary, raw, selected[label]);
      result.runs.push({
        label,
        ...run,
        childExitCode: child.code,
        childSignal: child.signal,
        childStderrPresent: child.stderrPresent,
      });
      await save();
      // Do not launch another workload while the first one's cleanup is uncertain.
      if (!run.cleaned)
        throw Error(
          "Child cleanup was not confirmed; second workload was not started",
        );
    }
    result.functionalStatus = result.runs.every(
      (r) => r.functionalStatus === "PASS" && r.audioPass,
    )
      ? "PASS"
      : "FAIL";
    result.status = result.runs.every(
      (r) => r.status === "PASS" && r.audioPass && r.childExitCode === 0,
    )
      ? "PASS"
      : "FAIL";
  } catch (error) {
    result.status = "FAIL";
    result.error = error.message;
  }
  result.finishedAt = new Date().toISOString();
  await save();
  console.log(
    JSON.stringify({
      status: result.status,
      functionalStatus: result.functionalStatus || "UNCONFIRMED",
      artifact: path.relative(root, dir),
    }),
  );
  if (result.status !== "PASS") process.exitCode = 1;
}
if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  await main();
