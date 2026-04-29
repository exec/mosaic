import {Match, Show, Switch} from 'solid-js';
import type {DetailDTO, InspectorTab} from '../../lib/bindings';
import type {BandwidthSample} from '../../lib/store';
import {InspectorHeader} from './InspectorHeader';
import {InspectorTabs} from './InspectorTabs';
import {OverviewTab} from './OverviewTab';
import {FilesTab} from './FilesTab';
import {PeersTab} from './PeersTab';
import {TrackersTab} from './TrackersTab';
import {SpeedTab} from './SpeedTab';

type Props = {
  open: boolean;
  detail: DetailDTO | null;
  tab: InspectorTab;
  bandwidth: BandwidthSample[];
  onTabChange: (t: InspectorTab) => void;
  onClose: () => void;
  onSetFilePriority: (index: number, priority: 'skip' | 'normal' | 'high' | 'max') => void;
};

export function Inspector(props: Props) {
  return (
    <Show when={props.open}>
      <aside class="flex h-full w-[420px] shrink-0 flex-col border-l border-white/[.04] bg-white/[.01] backdrop-blur-sm animate-in fade-in">
        <InspectorHeader detail={props.detail} onClose={props.onClose} />
        <div class="border-b border-white/[.04] px-3 py-2">
          <InspectorTabs active={props.tab} onChange={props.onTabChange} />
        </div>
        <div class="flex-1 overflow-auto">
          <Switch>
            <Match when={props.tab === 'overview'}>
              <OverviewTab detail={props.detail} />
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
              <SpeedTab samples={props.bandwidth} />
            </Match>
          </Switch>
        </div>
      </aside>
    </Show>
  );
}
