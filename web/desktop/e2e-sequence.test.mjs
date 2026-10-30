import { test } from "node:test";
import { deepStrictEqual, strictEqual } from "node:assert";
import { runSequential } from "../../test/e2e/sequence.mjs";

test("desktop verification stops before the next workload when a run fails", async () => {
  const seen = [];
  const result = await runSequential(["novnc", "neko"], async (target) => {
    seen.push(target);
    return false; // Includes failed or unconfirmed cleanup.
  });
  strictEqual(result, false);
  deepStrictEqual(seen, ["novnc"]);
});

test("desktop verification runs both only after confirmed success", async () => {
  const seen = [];
  const result = await runSequential(["novnc", "neko"], async (target) => {
    seen.push(target);
    return true;
  });
  strictEqual(result, true);
  deepStrictEqual(seen, ["novnc", "neko"]);
});

test("desktop verification does not start a cancelled workload", async () => {
  const seen = [];
  let cancelled = false;
  const result = await runSequential(
    ["novnc", "neko"],
    async (target) => {
      seen.push(target);
      cancelled = true;
      return true;
    },
    () => cancelled,
  );
  strictEqual(result, false);
  deepStrictEqual(seen, ["novnc"]);
});
