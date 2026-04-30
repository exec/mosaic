import {ExternalLink} from 'lucide-solid';

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

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4">
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
        <div class="text-xs uppercase tracking-wider text-zinc-500 mb-2">License & acknowledgements</div>
        <p class="text-sm text-zinc-300">
          Mosaic is open source software. The project is built on top of{' '}
          <a
            href="https://github.com/anacrolix/torrent"
            target="_blank"
            rel="noopener noreferrer"
            class="text-accent-400 hover:text-accent-200"
          >
            anacrolix/torrent
          </a>{' '}
          (BitTorrent engine),{' '}
          <a
            href="https://wails.io"
            target="_blank"
            rel="noopener noreferrer"
            class="text-accent-400 hover:text-accent-200"
          >
            Wails
          </a>{' '}
          (desktop shell), and{' '}
          <a
            href="https://www.solidjs.com"
            target="_blank"
            rel="noopener noreferrer"
            class="text-accent-400 hover:text-accent-200"
          >
            SolidJS
          </a>{' '}
          (UI). See the repository for the full list of dependencies and their licenses.
        </p>
      </div>
    </div>
  );
}
