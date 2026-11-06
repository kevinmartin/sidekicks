import { randomBytes } from "node:crypto";
import {
  readFileSync,
  openSync,
  writeSync,
  closeSync,
  fsyncSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";

const adapters = ["shell", "browser-chromium", "desktop-novnc", "desktop-neko"];
const exactID = /^sha256:[0-9a-f]{64}$/;
const validRun = /^[0-9a-f]{32}$/;

function imagesFromManifest(manifest, runId) {
  if (
    !manifest ||
    manifest.schemaVersion !== 1 ||
    manifest.status !== "built-not-verified" ||
    manifest.runId !== runId ||
    !Array.isArray(manifest.images) ||
    manifest.images.length !== adapters.length
  )
    throw Error("invalid candidate manifest");
  const images = new Map();
  const ids = new Set();
  for (const entry of manifest.images) {
    if (
      !entry ||
      !adapters.includes(entry.adapter) ||
      images.has(entry.adapter) ||
      !exactID.test(entry.id) ||
      ids.has(entry.id) ||
      entry.tag !== `sidekicks-compute/${entry.adapter}:verify-${runId}`
    )
      throw Error("candidate manifest contains an invalid or duplicate image");
    images.set(entry.adapter, entry.id);
    ids.add(entry.id);
  }
  return images;
}

// Explicit dependencies make the complete sequence testable without Docker.
export function verifyCandidate({
  runId,
  run,
  readManifest,
  fingerprint,
  record = () => {},
}) {
  if (!validRun.test(runId)) throw Error("invalid candidate run ID");
  const checkedSource = fingerprint();
  if (!/^[0-9a-f]{64}$/.test(checkedSource))
    throw Error("cannot establish source fingerprint before checks");
  run("check", "make", ["check"], {});
  if (fingerprint() !== checkedSource)
    throw Error("source changed while deterministic checks were running");
  run("build candidates", "make", ["images-candidate"], {
    SIDEKICKS_CANDIDATE_RUN_ID: runId,
  });
  const manifest = readManifest(runId);
  const images = imagesFromManifest(manifest, runId);
  const source = manifest.sourceFingerprintSHA256;
  if (!/^[0-9a-f]{64}$/.test(source))
    throw Error("candidate source fingerprint missing");
  const checkSource = () => {
    if (fingerprint() !== source)
      throw Error("candidate source changed during verification");
  };
  record({
    phase: "candidate build",
    status: "PASS",
    runId,
    source,
    images: Object.fromEntries(images),
  });
  try {
    if (source !== checkedSource)
      throw Error("candidate source differs from checked source");
    checkSource();
  } catch (error) {
    record({
      phase: "source continuity",
      status: "FAIL",
      error: error.message,
    });
    throw error;
  }
  const checkedRun = (phase, program, args, env) => {
    try {
      checkSource();
      run(phase, program, args, env);
      checkSource();
      record({ phase, status: "PASS" });
    } catch (error) {
      record({ phase, status: "FAIL", error: error.message });
      throw error;
    }
  };
  checkedRun("shell and browser integration", "make", ["integration"], {
    SIDEKICKS_TEST_SHELL_IMAGE: images.get("shell"),
    SIDEKICKS_TEST_BROWSER_IMAGE: images.get("browser-chromium"),
  });
  for (const [adapter, browser] of [
    ["desktop-novnc", "chromium"],
    ["desktop-neko", "firefox"],
  ]) {
    checkedRun(`${adapter} app lifecycle`, "make", ["integration-apps"], {
      SIDEKICKS_APPS_IMAGE_ID: images.get(adapter),
      SIDEKICKS_APPS_TEST_BROWSER: browser,
    });
    const args = [
      "test/e2e/desktop.mjs",
      "--execute",
      "--adapter",
      adapter,
      "--image",
      images.get(adapter),
    ];
    if (adapter === "desktop-neko") args.push("--audio");
    checkedRun(`${adapter} desktop E2E`, process.execPath, args, {
      SIDEKICKS_E2E: "1",
    });
  }
  return images;
}

function main() {
  const runId = randomBytes(16).toString("hex");
  let evidence;
  const run = (phase, program, args, overrides) => {
    console.log(`Verification: ${phase}`);
    const result = spawnSync(program, args, {
      stdio: "inherit",
      env: { ...process.env, ...overrides },
    });
    if (result.error || result.status !== 0)
      throw Error(
        `${phase} failed${result.error ? `: ${result.error.message}` : ` (exit ${result.status})`}`,
      );
  };
  try {
    verifyCandidate({
      runId,
      run,
      readManifest: (id) =>
        JSON.parse(
          readFileSync(
            `artifacts/compute/candidate-build/${id}/manifest.json`,
            "utf8",
          ),
        ),
      fingerprint: () => {
        const result = spawnSync(
          "bash",
          ["scripts/compute/source-fingerprint.sh"],
          { encoding: "utf8" },
        );
        if (result.error || result.status !== 0)
          throw Error("cannot verify candidate source fingerprint");
        return result.stdout.trim();
      },
      record: (entry) => {
        if (evidence === undefined)
          evidence = openSync(
            `artifacts/compute/candidate-build/${runId}/verification.jsonl`,
            "wx",
            0o600,
          );
        const line = Buffer.from(
          `${JSON.stringify({ ...entry, at: new Date().toISOString() })}\n`,
        );
        if (writeSync(evidence, line) !== line.length)
          throw Error("verification evidence write was incomplete");
        fsyncSync(evidence);
      },
    });
  } finally {
    if (evidence !== undefined) closeSync(evidence);
  }
  console.log(
    `Candidate verification passed: artifacts/compute/candidate-build/${runId}/manifest.json`,
  );
  console.log(
    "No image was promoted. The separate security release gate remains required.",
  );
}

if (
  process.argv[1] &&
  pathToFileURL(process.argv[1]).href === import.meta.url
) {
  try {
    main();
  } catch (error) {
    console.error(`Verification stopped: ${error.message}`);
    process.exitCode = 1;
  }
}
