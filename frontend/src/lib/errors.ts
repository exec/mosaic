// Trims an unknown thrown value (Error, string, anything) to a one-line
// human-friendly message suitable for a toast. Strips the noisy 'Error: '
// prefix the platform adds, and clamps long stacks so a verbose backend
// error doesn't blow up the toast UI.
//
// Use this anywhere an error reaches the user via toast.error / toast.warning
// instead of `String(e)` — the latter leaks raw multi-line stack traces.
export function userErr(e: unknown): string {
  const s = e instanceof Error ? e.message : String(e);
  const trimmed = s.replace(/^Error:\s*/, '').trim();
  return trimmed.length > 200 ? trimmed.slice(0, 197) + '…' : trimmed;
}
