import {lazy, Match, Show, Suspense, Switch} from 'solid-js';
import type {DetailDTO, InspectorTab} from '../../lib/bindings';
import type {BandwidthRing} from '../../lib/ringbuffer';
import {InspectorHeader} from './InspectorHeader';
import {InspectorTabs} from './InspectorTabs';
import {OverviewTab} from './OverviewTab';
import {FilesTab} from './FilesTab';
import {PeersTab} from './PeersTab';
import {TrackersTab} from './TrackersTab';

// SpeedTab pulls in uPlot (~45KB) and is only reached when the user opens
// the Speed tab — lazy-load it so the chart bundle stays off the hot path.
const SpeedTab = lazy(() => import('./SpeedTab').then((m) => ({default: m.SpeedTab})));

type Props = {
  open: boolean;
  detail: DetailDTO | null;
  // Live download rate from the matching Torrent in the list — see header.
  downloadRate: number;
  tab: InspectorTab;
  bandwidthRing: BandwidthRing;
  bandwidthTick: number;
  sequential: boolean;
  onTabChange: (t: InspectorTab) => void;
  onClose: () => void;
  onSetFilePriority: (index: number, priority: 'skip' | 'normal' | 'high' | 'max') => void;
  onToggleSequential: () => void;
};

export function Inspector(props: Props) {
  return (
    <Show when={props.open}>
      {/* pt-7 keeps the header (title + close X) below the drag overlay at
          the top of the window. Background paints to the top edge so the
          inspector visually extends top-to-bottom like the side rails. */}
      <aside class="flex h-full w-[420px] shrink-0 flex-col border-l border-white/[.04] bg-white/[.01] backdrop-blur-sm animate-in fade-in pt-7">
        <InspectorHeader detail={props.detail} downloadRate={props.downloadRate} onClose={props.onClose} />
        <div class="border-b border-white/[.04] px-3 py-2">
          <InspectorTabs active={props.tab} onChange={props.onTabChange} />
        </div>
        <div class="flex-1 overflow-auto">
          <Switch>
            <Match when={props.tab === 'overview'}>
              <OverviewTab
                detail={props.detail}
                sequential={props.sequential}
                onToggleSequential={props.onToggleSequential}
              />
            </Match>
            <Match when={props.tab === 'files'}>
              <FilesTab detail={props.detail} onSetPriority={props.onSetFilePriority} />
            </Match>
            <Match when={props.tab === 'peers'}>
              <PeersTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'trackers'}>
              <TrackersTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'speed'}>
              <Suspense fallback={<div class="p-4 text-xs text-zinc-500">Loading chart…</div>}>
                <SpeedTab ring={props.bandwidthRing} tick={props.bandwidthTick} />
              </Suspense>
            </Match>
          </Switch>
        </div>
      </aside>
    </Show>
  );
}
