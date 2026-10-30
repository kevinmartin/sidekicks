// Do not create another disposable workload after a failed or uncertain run.
// run() records its own cleanup outcome; any failure is sufficient to stop.
export async function runSequential(targets, run, cancelled = () => false) {
  for (const target of targets) {
    if (cancelled() || !(await run(target))) return false;
  }
  return !cancelled();
}
