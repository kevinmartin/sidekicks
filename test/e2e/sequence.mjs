// Do not create another disposable workload after a failed or uncertain run.
// run() records its own cleanup outcome; any failure is sufficient to stop.
export async function runSequential(targets, run, cancelled = () => false) {
  for (const target of targets) {
    if (cancelled() || !(await run(target))) return false;
  }
  return !cancelled();
}

// Preserve cancellation even if it arrives while the final result is being saved.
export async function saveFinalResult(result, save, cancelled) {
  const message = "Supervisor cancellation during cleanup";
  const markCancellation = () => {
    if (!result.cleanup.includes(message)) result.cleanup.push(message);
    result.status = "FAIL";
  };
  if (cancelled()) markCancellation();
  await save();
  if (cancelled() && !result.cleanup.includes(message)) {
    markCancellation();
    await save();
  }
}
