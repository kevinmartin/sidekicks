import { test } from "node:test";
import { deepStrictEqual, strictEqual, throws } from "node:assert";
import { verifyCandidate } from "../../scripts/compute/verify.mjs";

const runId = "0123456789abcdef0123456789abcdef";
const source = "a".repeat(64);
const adapters = ["shell", "browser-chromium", "desktop-novnc", "desktop-neko"];
function manifest() {
  return {
    schemaVersion: 1,
    status: "built-not-verified",
    runId,
    sourceFingerprintSHA256: source,
    images: adapters.map((adapter, i) => ({
      adapter,
      tag: `sidekicks-compute/${adapter}:verify-${runId}`,
      id: `sha256:${String(i + 1).repeat(64)}`,
    })),
  };
}
function fixture(overrides = {}) {
  const seen = [];
  const records = [];
  const options = {
    runId,
    readManifest: manifest,
    fingerprint: () => source,
    record: (item) => records.push(item),
    run: (phase, program, args, env) =>
      seen.push({ phase, program, args, env }),
    ...overrides,
  };
  return { seen, records, options };
}
test("verification binds every workload check to candidate IDs", () => {
  const f = fixture();
  verifyCandidate(f.options);
  deepStrictEqual(
    f.seen.map((s) => s.phase),
    [
      "check",
      "build candidates",
      "shell and browser integration",
      "desktop-novnc app lifecycle",
      "desktop-novnc desktop E2E",
      "desktop-neko app lifecycle",
      "desktop-neko desktop E2E",
    ],
  );
  strictEqual(f.seen[1].env.SIDEKICKS_CANDIDATE_RUN_ID, runId);
  strictEqual(
    f.seen[2].env.SIDEKICKS_TEST_SHELL_IMAGE,
    manifest().images[0].id,
  );
  strictEqual(
    f.seen[2].env.SIDEKICKS_TEST_BROWSER_IMAGE,
    manifest().images[1].id,
  );
  strictEqual(f.seen[3].env.SIDEKICKS_APPS_IMAGE_ID, manifest().images[2].id);
  strictEqual(f.seen[5].env.SIDEKICKS_APPS_IMAGE_ID, manifest().images[3].id);
  strictEqual(f.seen[4].args.includes(manifest().images[2].id), true);
  strictEqual(f.seen[6].args.includes(manifest().images[3].id), true);
  strictEqual(f.seen[6].args.includes("--audio"), true);
  strictEqual(f.records.length, 6);
});
test("verification rejects mixed, mutable or duplicate manifest identities", () => {
  for (const mutate of [
    (m) => {
      m.images[0].id = "shell:latest";
    },
    (m) => {
      m.images[1].id = m.images[0].id;
    },
    (m) => {
      m.images[0].tag = "sidekicks-compute/shell:clean";
    },
    (m) => {
      m.runId = "f".repeat(32);
    },
  ]) {
    const m = manifest();
    mutate(m);
    const f = fixture({ readManifest: () => m });
    throws(() => verifyCandidate(f.options));
    strictEqual(f.seen.length, 2);
  }
});
test("verification halts and records failure without starting later workloads", () => {
  const f = fixture();
  f.options.run = (phase, ...rest) => {
    f.seen.push({ phase, rest });
    if (phase === "desktop-novnc app lifecycle")
      throw Error("cleanup uncertain");
  };
  throws(() => verifyCandidate(f.options), /cleanup uncertain/);
  strictEqual(f.seen.at(-1).phase, "desktop-novnc app lifecycle");
  strictEqual(f.records.at(-1).status, "FAIL");
});
test("verification rejects source drift before and after a workload check", () => {
  for (const failAt of [1, 3]) {
    let calls = 0;
    const f = fixture({
      fingerprint: () => (++calls === failAt ? "b".repeat(64) : source),
    });
    throws(() => verifyCandidate(f.options), /source changed/);
    strictEqual(
      f.seen.some((s) => s.phase === "desktop-novnc app lifecycle"),
      false,
    );
  }
});
