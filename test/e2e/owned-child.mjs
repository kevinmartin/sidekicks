// Bound an already-spawned owned child. Cancellation is cooperative first;
// forced termination or missing result can never establish guest cleanup.
export function collectOwnedChild(
  child,
  { deadlineMs = 600_000, graceMs = 60_000, killWaitMs = 5_000 } = {},
) {
  return new Promise((resolve, reject) => {
    let stdout = "",
      stderr = "",
      outputBytes = 0,
      reason = "",
      timer,
      grace,
      last;
    let settled = false;
    const finish = (error, value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      clearTimeout(grace);
      clearTimeout(last);
      if (error) reject(error);
      else resolve(value);
    };
    const cancel = (why) => {
      if (reason) return;
      reason = why;
      child.kill("SIGTERM");
      grace = setTimeout(() => {
        child.kill("SIGKILL");
        last = setTimeout(
          () =>
            finish(
              Error(`${reason}; child termination and cleanup are unconfirmed`),
            ),
          killWaitMs,
        );
      }, graceMs);
    };
    timer = setTimeout(
      () => cancel("Owned child exceeded its deadline"),
      deadlineMs,
    );
    const record = (which) => (buffer) => {
      if (reason) return;
      outputBytes += buffer.length;
      if (outputBytes > 1 << 20) {
        cancel("Owned child exceeded output bound");
      } else if (which === "stdout") stdout += buffer.toString("utf8");
      else stderr += buffer.toString("utf8");
    };
    child.stdout.on("data", record("stdout"));
    child.stderr.on("data", record("stderr"));
    child.once("error", (error) =>
      finish(
        Error(`Owned child error: ${error.message}; cleanup is unconfirmed`),
      ),
    );
    child.once("close", (code, signal) => {
      if (reason)
        return finish(
          Error(
            `${reason}; cleanup is unconfirmed; inspect its saved result before further work`,
          ),
        );
      try {
        const summary = JSON.parse(
          stdout.trim().split("\n").filter(Boolean).at(-1),
        );
        finish(null, { code, signal, summary, stderrPresent: !!stderr.trim() });
      } catch {
        finish(
          Error(
            `No bounded child result (exit ${code}, signal ${signal || "none"}); cleanup is unconfirmed`,
          ),
        );
      }
    });
  });
}

// launchServer uses the spawned PID as a separate Unix process-group ID.
// Signal 0 observes that group without sending it a signal.
export function inspectOwnedProcessGroup(
  pid,
  platform = process.platform,
  kill = process.kill,
) {
  if (!Number.isSafeInteger(pid) || pid <= 1 || platform === "win32")
    return {
      absent: false,
      error: "unsupported or invalid owned process group",
    };
  try {
    kill(-pid, 0);
    return { absent: false, error: "owned process group still exists" };
  } catch (error) {
    if (error?.code === "ESRCH") return { absent: true };
    return {
      absent: false,
      error: `owned process group check failed: ${error?.code || "unknown"}`,
    };
  }
}

function deadline(promise, timeoutMs, label) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(Error(`${label} timed out`)), timeoutMs);
    }),
  ]).finally(() => clearTimeout(timer));
}

// BrowserServer.kill waits for Node's ChildProcess "close" event, which also
// waits for every inherited pipe to close after the process has exited. First
// require actual exit and absence of the owned process group, then close only
// this ChildProcess's local pipe endpoints and still require Playwright's own
// kill/temporary-directory cleanup promise to finish. Never close pipes while
// the leader or group might still be alive.
export async function cleanupOwnedBrowserServer(
  server,
  {
    exitMs = 10000,
    groupMs = 5000,
    cleanupMs = 5000,
    probe = inspectOwnedProcessGroup,
  } = {},
) {
  const child = server.process();
  if (
    !Number.isSafeInteger(child?.pid) ||
    child.pid <= 1 ||
    !Array.isArray(child.stdio)
  )
    throw Error("Owned Chrome process identity or pipes unavailable");
  const result = {
    pid: child.pid,
    method: "BrowserServer.kill",
    pipesClosed: 0,
  };
  let onExit;
  const exited = () =>
    Number.isInteger(child.exitCode) || typeof child.signalCode === "string";
  const exitPromise = new Promise((resolve) => {
    onExit = () => resolve();
    child.once("exit", onExit);
    if (exited()) resolve();
  });
  // Convert rejection to a value immediately, even while waiting for exit.
  const killPromise = Promise.resolve()
    .then(() => server.kill())
    .then(
      () => ({ ok: true }),
      (error) => ({ ok: false, error }),
    );
  try {
    await deadline(exitPromise, exitMs, "owned Chrome exit");
  } finally {
    child.removeListener("exit", onExit);
  }
  result.exitCode = child.exitCode;
  result.signalCode = child.signalCode;
  const end = Date.now() + groupMs;
  let group;
  const observations = [];
  do {
    group = probe(child.pid);
    if (observations.length < 101) observations.push(group);
    if (group.absent) break;
    // EPERM may occur while a sandboxed Chrome descendant is still exiting.
    // It is never evidence of absence; retry only within the same deadline.
    if (
      group.error !== "owned process group still exists" &&
      group.error !== "owned process group check failed: EPERM"
    )
      throw Error(group.error || "Owned Chrome process group is unconfirmed");
    await new Promise((resolve) => setTimeout(resolve, 50));
  } while (Date.now() < end);
  if (!group.absent)
    throw Error(
      group.error === "owned process group still exists"
        ? "Owned Chrome process group remained after exit"
        : group.error || "Owned Chrome process group is unconfirmed",
    );
  result.processGroup = group;
  result.processGroupObservations = observations;
  for (const pipe of child.stdio) {
    if (!pipe || pipe.destroyed) continue;
    if (typeof pipe.destroy !== "function")
      throw Error("Owned Chrome pipe cannot be closed safely");
    pipe.destroy();
    result.pipesClosed++;
  }
  const outcome = await deadline(
    killPromise,
    cleanupMs,
    "owned browser server cleanup",
  );
  if (!outcome.ok)
    throw Error(
      `Owned browser server cleanup failed: ${outcome.error?.message || "unknown"}`,
    );
  result.playwrightCleanupCompleted = true;
  return result;
}
