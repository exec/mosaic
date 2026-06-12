export function fmtBytes(n: number): string {
  if (!Number.isFinite(n)) return '—';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function fmtRate(bytesPerSec: number): string {
  if (bytesPerSec < 1) return '—';
  return `${fmtBytes(bytesPerSec)}/s`;
}

export function fmtETA(remainingBytes: number, bytesPerSec: number): string {
  if (!Number.isFinite(remainingBytes) || !Number.isFinite(bytesPerSec)) return '—';
  if (bytesPerSec === 0) return '∞';
  const seconds = remainingBytes / bytesPerSec;
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86_400)}d`;
}

export function fmtPercent(progress: number): string {
  if (progress >= 1) return '100%';
  // toFixed rounds, so progress in [0.9995, 1) would still render "100.0%"
  // despite the guard above — clamp so a not-quite-done torrent never
  // claims completion.
  return `${Math.min(progress * 100, 99.9).toFixed(1)}%`;
}

// Floor (never round) the minor unit: rounding produced impossible
// renderings like "1h 60m" (3599s of remainder) and "1d 24h".
export function fmtDuration(seconds: number): string {
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86_400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds - h * 3600) / 60);
    return `${h}h ${m}m`;
  }
  const d = Math.floor(seconds / 86_400);
  const h = Math.floor((seconds - d * 86_400) / 3600);
  return `${d}d ${h}h`;
}

export function fmtTimestamp(unixSeconds: number): string {
  if (!unixSeconds) return '—';
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleString();
}
