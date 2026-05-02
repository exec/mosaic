import {createEffect, createSignal, Show} from 'solid-js';
import {Download, RefreshCw} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {UpdaterConfigDTO, UpdateInfoDTO} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';
import {Button} from '../ui/Button';

type Props = {
  config: UpdaterConfigDTO;
  info: UpdateInfoDTO | null;
  appVersion: string;
  onSet: (c: UpdaterConfigDTO) => Promise<void>;
  onCheck: () => Promise<UpdateInfoDTO>;
  onInstall: () => Promise<void>;
};

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

export function UpdatesPane(props: Props) {
  const [enabled, setEnabled] = createSignal(props.config.enabled);
  const [channel, setChannel] = createSignal<'stable' | 'beta'>(props.config.channel);
  const [checking, setChecking] = createSignal(false);
  const [installing, setInstalling] = createSignal(false);

  // Re-sync local form state when props change (initial fetch races).
  createEffect(() => { setEnabled(props.config.enabled); });
  createEffect(() => { setChannel(props.config.channel); });

  const dirty = () =>
    enabled() !== props.config.enabled || channel() !== props.config.channel;

  const save = async () => {
    try {
      await props.onSet({...props.config, enabled: enabled(), channel: channel()});
      toast.success('Update settings saved');
    } catch (e) {
      toast.error(String(e));
    }
  };

  const check = async () => {
    setChecking(true);
    try {
      await props.onCheck();
      toast.success('Check complete');
    } catch (e) {
      toast.error(String(e));
    } finally {
      setChecking(false);
    }
  };

  const install = async () => {
    setInstalling(true);
    try {
      await props.onInstall();
      toast.success('Installed — relaunch Mosaic');
    } catch (e) {
      toast.error(String(e));
    } finally {
      setInstalling(false);
    }
  };

  const lastCheckedLabel = () => {
    if (!props.config.last_checked_at) return 'never';
    return new Date(props.config.last_checked_at * 1000).toLocaleString();
  };

  const aptManaged = () => props.config.install_source === 'apt';

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader
        title="Updates"
        subtitle={
          aptManaged()
            ? 'This Mosaic is installed by apt — the system package manager owns upgrades.'
            : 'Mosaic checks for new versions on startup and every 24 hours.'
        }
      />

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4">
        <div class="flex items-center justify-between">
          <span class="text-sm text-zinc-300">Current version</span>
          <span class="font-mono text-sm tabular-nums text-zinc-100" data-testid="updater-version">
            {props.appVersion}
          </span>
        </div>
        <Show when={!aptManaged()}>
          <div class="mt-2 flex items-center justify-between">
            <span class="text-sm text-zinc-300">Last checked</span>
            <span class="text-sm text-zinc-400" data-testid="updater-last-checked">
              {lastCheckedLabel()}
            </span>
          </div>
        </Show>
        <Show when={!aptManaged() && props.info?.available}>
          <div class="mt-3 rounded-md bg-accent-500/10 p-3 text-sm" data-testid="updater-available">
            <div class="text-accent-200">
              Update available: <span class="font-mono">{props.info!.latest_version}</span>
            </div>
            <button
              type="button"
              class="mt-2 inline-flex items-center gap-1.5 rounded-md bg-accent-500 px-3 py-1 text-sm font-medium text-zinc-950 hover:bg-accent-400 disabled:opacity-50"
              disabled={installing() || !isWailsRuntime()}
              onClick={install}
              data-testid="updater-install"
            >
              <Download class="h-3.5 w-3.5" />
              {installing() ? 'Installing…' : 'Install update'}
            </button>
            <Show when={!isWailsRuntime()}>
              <p class="mt-1 text-xs text-zinc-500">Install must run from the desktop app.</p>
            </Show>
          </div>
        </Show>
      </div>

      <Show
        when={!aptManaged()}
        fallback={
          <div
            class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4 text-sm text-zinc-300"
            data-testid="updater-apt-managed"
          >
            <p class="mb-2 font-medium text-zinc-100">Managed by apt</p>
            <p class="mb-2 leading-relaxed text-zinc-400">
              Run the system package manager to upgrade — Mosaic's in-app updater is disabled because it can't keep dpkg's view of the installed version in sync.
            </p>
            <pre class="rounded bg-black/40 p-2 text-xs text-zinc-200">sudo apt update &amp;&amp; sudo apt upgrade mosaic</pre>
          </div>
        }
      >
        <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4">
          <label class="flex items-center gap-2 text-sm text-zinc-200 cursor-pointer">
            <input
              type="checkbox"
              checked={enabled()}
              onChange={(e) => setEnabled(e.currentTarget.checked)}
              class="accent-accent-500"
              data-testid="updater-enabled"
            />
            Enable automatic update checks
          </label>

          <fieldset class="mt-4">
            <legend class="text-xs uppercase tracking-wider text-zinc-500 mb-1">Channel</legend>
            <div class="flex gap-4 text-sm text-zinc-200">
              <label class="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="radio"
                  name="channel"
                  checked={channel() === 'stable'}
                  onChange={() => setChannel('stable')}
                  class="accent-accent-500"
                  data-testid="updater-channel-stable"
                />
                Stable
              </label>
              <label class="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="radio"
                  name="channel"
                  checked={channel() === 'beta'}
                  onChange={() => setChannel('beta')}
                  class="accent-accent-500"
                  data-testid="updater-channel-beta"
                />
                Beta
              </label>
            </div>
          </fieldset>
        </div>

        <div class="flex items-center gap-2">
          <button
            type="button"
            onClick={check}
            disabled={checking()}
            class="inline-flex items-center gap-1.5 rounded-md border border-white/[.06] bg-white/[.04] px-3 py-1.5 text-sm text-zinc-200 hover:bg-white/[.06] disabled:opacity-50"
            data-testid="updater-check"
          >
            <RefreshCw class={`h-3.5 w-3.5 ${checking() ? 'animate-spin' : ''}`} />
            Check now
          </button>
          <div class="flex-1" />
          <Button variant="primary" onClick={save} disabled={!dirty()} data-testid="updater-save">
            Save
          </Button>
        </div>
      </Show>
    </div>
  );
}
