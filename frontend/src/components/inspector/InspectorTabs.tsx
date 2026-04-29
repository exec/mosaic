import {ToggleGroup} from '@kobalte/core/toggle-group';
import type {InspectorTab} from '../../lib/bindings';

const labels: {value: InspectorTab; label: string}[] = [
  {value: 'overview', label: 'Overview'},
  {value: 'files',    label: 'Files'},
  {value: 'peers',    label: 'Peers'},
  {value: 'trackers', label: 'Trackers'},
  {value: 'speed',    label: 'Speed'},
];

type Props = {
  active: InspectorTab;
  onChange: (t: InspectorTab) => void;
};

export function InspectorTabs(props: Props) {
  return (
    <ToggleGroup
      class="flex w-full items-center gap-px rounded-md border border-white/[.06] bg-white/[.02] p-0.5"
      value={props.active}
      onChange={(v) => v && props.onChange(v as InspectorTab)}
    >
      {labels.map((it) => (
        <ToggleGroup.Item
          value={it.value}
          class="flex-1 rounded px-2 py-1 text-xs text-zinc-400 transition-colors duration-100 hover:text-zinc-100 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100"
        >
          {it.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup>
  );
}
