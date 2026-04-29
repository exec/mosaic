export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function fmtRate(bytesPerSec: number): string {
  if (bytesPerSec === 0) return '—';
  return `${fmtBytes(bytesPerSec)}/s`;
}

export function fmtETA(remainingBytes: number, bytesPerSec: number): string {
  if (bytesPerSec === 0) return '∞';
  const seconds = remainingBytes / bytesPerSec;
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86_400)}d`;
}

export function fmtPercent(progress: number): string {
  if (progress >= 1) return '100%';
  return `${(progress * 100).toFixed(1)}%`;
}

export function fmtDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86_400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds - h * 3600) / 60);
    return `${h}h ${m}m`;
  }
  const d = Math.floor(seconds / 86_400);
  const h = Math.round((seconds - d * 86_400) / 3600);
  return `${d}d ${h}h`;
}

export function fmtTimestamp(unixSeconds: number): string {
  if (!unixSeconds) return '—';
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleString();
}
