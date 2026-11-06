// Independent bounded PCM checks. No browser, host audio or process access.
export function makeToneWAV() {
  const sampleRate = 16000,
    channels = 1,
    durationMs = 500,
    frequencyHz = 440,
    amplitude = 0.1;
  const count = (sampleRate * durationMs) / 1000,
    dataBytes = count * 2;
  const wav = Buffer.alloc(44 + dataBytes);
  wav.write("RIFF", 0);
  wav.writeUInt32LE(wav.length - 8, 4);
  wav.write("WAVEfmt ", 8);
  wav.writeUInt32LE(16, 16);
  wav.writeUInt16LE(1, 20);
  wav.writeUInt16LE(channels, 22);
  wav.writeUInt32LE(sampleRate, 24);
  wav.writeUInt32LE(sampleRate * channels * 2, 28);
  wav.writeUInt16LE(channels * 2, 32);
  wav.writeUInt16LE(16, 34);
  wav.write("data", 36);
  wav.writeUInt32LE(dataBytes, 40);
  for (let i = 0; i < count; i++)
    wav.writeInt16LE(
      Math.round(
        Math.sin((2 * Math.PI * frequencyHz * i) / sampleRate) *
          32767 *
          amplitude,
      ),
      44 + i * 2,
    );
  return { wav, sampleRate, channels, durationMs, frequencyHz, amplitude };
}
export function analyzePCM(samples, sampleRate) {
  if (
    !Array.isArray(samples) ||
    samples.length < 512 ||
    samples.length > 16384 ||
    !Number.isFinite(sampleRate) ||
    sampleRate < 8000 ||
    sampleRate > 96000 ||
    samples.some((v) => !Number.isFinite(v) || Math.abs(v) > 1.1)
  )
    throw Error("Invalid bounded decoded PCM");
  const n = samples.length,
    weighted = new Float64Array(n);
  let power = 0,
    peak = 0,
    weight = 0;
  for (let i = 0; i < n; i++) {
    power += samples[i] * samples[i];
    peak = Math.max(peak, Math.abs(samples[i]));
    const w = 0.5 - 0.5 * Math.cos((2 * Math.PI * i) / (n - 1));
    weighted[i] = samples[i] * w;
    weight += w;
  }
  const rms = Math.sqrt(power / n);
  const magnitude = (f) => {
    const coefficient = 2 * Math.cos((2 * Math.PI * f) / sampleRate);
    let a = 0,
      b = 0;
    for (const sample of weighted) {
      const v = sample + coefficient * a - b;
      b = a;
      a = v;
    }
    return Math.max(0, a * a + b * b - coefficient * a * b);
  };
  let frequency = 0,
    best = 0;
  for (let f = 80; f <= 1500; f += 5) {
    const v = magnitude(f);
    if (v > best) {
      best = v;
      frequency = f;
    }
  }
  const coarse = frequency;
  for (let f = Math.max(80, coarse - 5); f <= Math.min(1500, coarse + 5); f++) {
    const v = magnitude(f);
    if (v > best) {
      best = v;
      frequency = f;
    }
  }
  const toneRMS = Math.sqrt(2 * best) / weight;
  return {
    sampleCount: n,
    sampleRate,
    rms,
    peakAbs: peak,
    dominantHz: rms > 1e-8 ? frequency : null,
    dominantRMS: toneRMS,
    dominantToTotalRMS: rms ? toneRMS / rms : 0,
  };
}
export function verifyTone(baseline, capture) {
  for (const value of [baseline, capture]) {
    if (
      !Array.isArray(value?.readings) ||
      value.readings.length < 2 ||
      value.readings.length > 260 ||
      value.readings.some(
        (r, i) =>
          !Number.isFinite(r.atMs) ||
          r.atMs < 0 ||
          !Number.isFinite(r.rms) ||
          r.rms < 0 ||
          r.rms > 1.1 ||
          (i > 0 && r.atMs < value.readings[i - 1].atMs),
      )
    )
      throw Error("Invalid bounded PCM observation window");
  }
  const analysis = analyzePCM(capture.bestPCM, capture.sampleRate);
  const baselinePeak = Math.max(0, ...baseline.readings.map((r) => r.rms));
  const energetic = capture.readings.filter(
    (r) => r.rms >= Math.max(0.006, baselinePeak * 8),
  );
  if (baselinePeak > 0.002)
    throw Error("Silent negative-control baseline was not quiet");
  if (
    analysis.rms < 0.006 ||
    analysis.rms < baselinePeak * 8 ||
    analysis.dominantHz === null ||
    Math.abs(analysis.dominantHz - 440) > 15 ||
    analysis.dominantToTotalRMS < 0.6 ||
    energetic.length < 3 ||
    energetic.at(-1).atMs - energetic[0].atMs < 60
  )
    throw Error("Decoded PCM did not establish the synthetic 440Hz tone");
  return {
    ...analysis,
    baselinePeakRMS: baselinePeak,
    energeticReadings: energetic.length,
    energeticSpanMs: energetic.at(-1).atMs - energetic[0].atMs,
  };
}

export function validateImageSelection(image, adapter) {
  if (
    !["desktop-novnc", "desktop-neko"].includes(adapter) ||
    typeof image !== "string" ||
    !/^sha256:[0-9a-f]{64}$/.test(image)
  )
    throw Error(
      "Image override requires one explicit adapter and an immutable local sha256 image ID",
    );
  return image;
}
