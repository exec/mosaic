import {Show} from 'solid-js';
import type {AppState} from '../../lib/store';
import {fmtTimestamp} from '../../lib/format';

export function ConnectionBanner(props: {connection?: AppState['connection']; onRetry?: () => void}) {
  const state = () => props.connection;
  return <Show when={state()?.stale}>
    <div role="status" class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-amber-500/15 bg-amber-500/[.06] px-4 py-2 text-xs text-amber-200/80">
      <span class="h-1.5 w-1.5 rounded-full bg-amber-400" />
      <span>{state()?.status === 'connecting' ? 'Connecting…' : state()?.status === 'reconnecting' ? 'Reconnecting…' : state()?.syncing ? 'Refreshing data…' : 'Refresh failed'}</span>
      <span class="text-zinc-500">{state()?.lastUpdateAt ? `Last update ${fmtTimestamp(state()!.lastUpdateAt / 1000)}` : 'Waiting for the first update'} · Displayed data may be out of date</span>
      <Show when={state()?.status === 'connected' && !state()?.syncing}><button type="button" class="ml-auto rounded px-2 py-1 text-amber-200 hover:bg-white/[.05]" onClick={props.onRetry} title={state()?.error}>Retry refresh</button></Show>
    </div>
  </Show>;
}
