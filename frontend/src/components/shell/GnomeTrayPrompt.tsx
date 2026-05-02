import {Match, Show, Switch, createSignal, onMount} from 'solid-js';
import {api} from '../../lib/bindings';

// One-time prompt shown to Linux+Gnome users when their session lacks a
// StatusNotifierWatcher (i.e., Mosaic's tray icon won't render). The
// backend tells us exactly what's wrong via api.gnomeTrayStatus():
//   - "needs_install": AppIndicator extension isn't on disk. We can't
//                      install it (apt needs root); show the apt copy-paste.
//   - "needs_enable":  extension installed but not in user's enabled list.
//                      Click the button → backend runs `gnome-extensions
//                      enable …` → user logs out + back in.
//   - "needs_restart": extension enabled in dconf but gnome-shell hasn't
//                      reloaded yet (e.g. user toggled it from elsewhere
//                      before launching Mosaic). Just tell them to restart.
//   - "not_applicable": off-Linux, off-Gnome, prompt previously dismissed,
//                       or watcher already serving — nothing to do.
//
// "Don't show again" persists in settings via DismissGnomeTrayPrompt.
type Status = 'not_applicable' | 'needs_install' | 'needs_enable' | 'needs_restart' | 'enabling';

export function GnomeTrayPrompt() {
  const [status, setStatus] = createSignal<Status>('not_applicable');
  const [enableError, setEnableError] = createSignal<string | null>(null);
  const [enabled, setEnabled] = createSignal(false);

  onMount(async () => {
    try {
      const s = (await api.gnomeTrayStatus()) as Status;
      if (s === 'needs_install' || s === 'needs_enable' || s === 'needs_restart') {
        setStatus(s);
      }
    } catch {
      // Browser-mode / non-Wails / older backend without the binding —
      // silently no-op. We don't want a console error in the typical
      // remote-WS use case.
    }
  });

  const onEnable = async () => {
    setEnableError(null);
    setStatus('enabling');
    try {
      await api.enableGnomeTray();
      setEnabled(true);
      setStatus('needs_restart');
    } catch (err) {
      setEnableError(String(err));
      setStatus('needs_enable');
    }
  };

  const onDismiss = async () => {
    try {
      await api.dismissGnomeTrayPrompt();
    } catch {
      // Don't block the dismiss UX on backend errors — worst case the
      // prompt comes back next launch.
    }
    setStatus('not_applicable');
  };

  return (
    <Show when={status() !== 'not_applicable'}>
      <div
        class="fixed bottom-4 right-4 z-50 max-w-md rounded-lg border border-amber-500/30 bg-zinc-900/95 p-4 shadow-xl backdrop-blur"
        role="status"
        aria-live="polite"
      >
        <div class="mb-2 flex items-center gap-2">
          <span class="text-amber-400" aria-hidden="true">⚠</span>
          <span class="text-sm font-semibold text-zinc-100">Tray icon not visible</span>
        </div>
        <Switch>
          <Match when={status() === 'needs_install'}>
            <p class="mb-3 text-xs leading-relaxed text-zinc-300">
              Gnome ignores Mosaic's tray icon protocol by default. Install the AppIndicator extension to see the tray icon and enable close-to-tray:
            </p>
            <pre class="mb-3 rounded bg-black/40 p-2 text-xs text-zinc-200">sudo apt install gnome-shell-extension-appindicator</pre>
            <p class="mb-3 text-xs leading-relaxed text-zinc-400">
              After install, enable it in the Extensions app (or run <code class="rounded bg-black/40 px-1">gnome-extensions enable {AppIndicatorUUID}</code>) and log out + back in.
            </p>
          </Match>
          <Match when={status() === 'needs_enable'}>
            <p class="mb-3 text-xs leading-relaxed text-zinc-300">
              The AppIndicator extension is installed but not enabled. Enable it now? You'll need to log out + back in for the tray to appear.
            </p>
            <Show when={enableError()}>
              <p class="mb-3 rounded bg-rose-950/60 p-2 text-xs text-rose-200">{enableError()}</p>
            </Show>
          </Match>
          <Match when={status() === 'needs_restart'}>
            <p class="mb-3 text-xs leading-relaxed text-zinc-300">
              <Show when={enabled()} fallback="The AppIndicator extension is enabled but Gnome hasn't picked it up yet.">
                The AppIndicator extension is now enabled.
              </Show>{' '}
              Log out and back in (or restart Gnome shell) to see Mosaic's tray icon.
            </p>
          </Match>
          <Match when={status() === 'enabling'}>
            <p class="mb-3 text-xs leading-relaxed text-zinc-300">Enabling…</p>
          </Match>
        </Switch>
        <div class="flex justify-end gap-2">
          <Show when={status() === 'needs_enable'}>
            <button
              type="button"
              onClick={onEnable}
              class="rounded bg-amber-500 px-3 py-1 text-xs font-medium text-zinc-950 transition-colors hover:bg-amber-400"
            >
              Enable
            </button>
          </Show>
          <button
            type="button"
            onClick={onDismiss}
            class="rounded border border-zinc-700 px-3 py-1 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800"
          >
            <Show when={status() === 'needs_restart'} fallback="Don't show again">
              Got it
            </Show>
          </button>
        </div>
      </div>
    </Show>
  );
}

const AppIndicatorUUID = 'appindicatorsupport@rgcjonas.gmail.com';
