import {ExternalLink} from 'lucide-solid';

const PROGRESS = [
  {label: 'Plan 1 — Foundation & first download', done: true},
  {label: 'Plan 2 — Polished window shell', done: true},
  {label: 'Plan 3 — Inspector + Watch IPC', done: true},
  {label: 'Plan 4a — Organization', done: true},
  {label: 'Plan 4b — Settings panel', done: true},
  {label: 'Plan 4c — Bandwidth controls', done: true},
  {label: 'Plan 4d — Scheduling & blocklist', done: true},
  {label: 'Plan 5 — RSS auto-add', done: true},
  {label: 'Plan 6 — Remote interface', done: true},
  {label: 'Plan 7 — Auto-update', done: true},
  {label: 'Plan 8 — Packaging & signing', done: false},
];

type Props = {
  appVersion: string;
};

export function AboutPane(props: Props) {
  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <div class="mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">About Mosaic</h2>
        <p class="mt-0.5 text-sm text-zinc-500">A polished cross-platform BitTorrent client — Go + Wails + anacrolix.</p>
        <p class="mt-2 text-xs font-mono text-zinc-400">
          Version <span class="text-zinc-200">{props.appVersion}</span>
        </p>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-6">
        <a
          href="https://github.com/exec/mosaic"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1.5 text-sm text-accent-400 hover:text-accent-200"
        >
          github.com/exec/mosaic
          <ExternalLink class="h-3 w-3" />
        </a>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4">
        <div class="text-xs uppercase tracking-wider text-zinc-500 mb-3">Roadmap</div>
        <ul class="flex flex-col gap-1.5 text-sm">
          {PROGRESS.map((p) => (
            <li class="flex items-center gap-2">
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                classList={{
                  'bg-seed': p.done,
                  'bg-zinc-700': !p.done,
                }}
              />
              <span classList={{'text-zinc-200': p.done, 'text-zinc-500': !p.done}}>{p.label}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
