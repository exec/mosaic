import {ToggleGroup} from '@kobalte/core/toggle-group';
import {Monitor, Moon, Sun} from 'lucide-solid';
import {useTheme} from './ThemeProvider';
import type {Theme} from '../../lib/theme';

const items: {value: Theme; label: string; icon: typeof Sun}[] = [
  {value: 'light', label: 'Light', icon: Sun},
  {value: 'dark', label: 'Dark', icon: Moon},
  {value: 'system', label: 'System', icon: Monitor},
];

export function ThemeToggle() {
  const {theme, setTheme} = useTheme();

  return (
    <ToggleGroup
      class="inline-flex items-center rounded-md border border-white/10 bg-white/[.02] p-0.5"
      value={theme()}
      onChange={(v) => v && setTheme(v as Theme)}
    >
      {items.map((item) => (
        <ToggleGroup.Item
          value={item.value}
          aria-label={item.label}
          class="grid h-7 w-7 place-items-center rounded text-zinc-400 transition-colors duration-150 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100 hover:text-zinc-100"
        >
          <item.icon class="h-3.5 w-3.5" />
        </ToggleGroup.Item>
      ))}
    </ToggleGroup>
  );
}
