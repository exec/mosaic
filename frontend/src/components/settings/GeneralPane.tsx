import {createEffect, createSignal} from 'solid-js';
import {toast} from 'solid-sonner';
import {Button} from '../ui/Button';
import {api, type WatchFolderDTO} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';

type Props = {
  defaultSavePath: string;
  onSetDefaultSavePath: (path: string) => Promise<void>;
  watchFolder: WatchFolderDTO;
  onSetWatchFolder: (c: WatchFolderDTO) => Promise<void>;
};

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

function Field(props: {label: string; help?: string; children: any}) {
  return (
    <div class="grid grid-cols-[200px_1fr] items-start gap-4 py-3 border-b border-white/[.03]">
      <div>
        <div class="text-sm text-zinc-200">{props.label}</div>
        {props.help && <div class="mt-0.5 text-xs text-zinc-500">{props.help}</div>}
      </div>
      <div>{props.children}</div>
    </div>
  );
}

export function GeneralPane(props: Props) {
  const [savePath, setSavePath] = createSignal(props.defaultSavePath);
  // Re-sync when the prop arrives later — the boot fetch in store.ts is
  // async, so a user landing here first sees an empty input until it lands.
  createEffect(() => { setSavePath(props.defaultSavePath); });
  const dirty = () => savePath() !== props.defaultSavePath;

  const save = async () => {
    try {
      await props.onSetDefaultSavePath(savePath());
      toast.success('Default save path updated');
    } catch (e) {
      toast.error(String(e));
    }
  };

  // Watch folder state — local draft, synced from props on external update.
  const [wfEnabled, setWfEnabled] = createSignal(props.watchFolder.enabled);
  const [wfPath, setWfPath] = createSignal(props.watchFolder.path);
  const [wfDelete, setWfDelete] = createSignal(props.watchFolder.delete_after_add);

  createEffect(() => {
    setWfEnabled(props.watchFolder.enabled);
    setWfPath(props.watchFolder.path);
    setWfDelete(props.watchFolder.delete_after_add);
  });

  const wfDirty = () =>
    wfEnabled() !== props.watchFolder.enabled ||
    wfPath() !== props.watchFolder.path ||
    wfDelete() !== props.watchFolder.delete_after_add;

  const saveWatchFolder = async () => {
    try {
      await props.onSetWatchFolder({
        path: wfPath(),
        delete_after_add: wfDelete(),
        enabled: wfEnabled(),
      });
      toast.success('Watch folder settings saved');
    } catch (e) {
      toast.error(String(e));
    }
  };

  const browseWatchFolder = async () => {
    try {
      const picked = await api.pickWatchFolder();
      if (picked) setWfPath(picked);
    } catch (e) {
      toast.error(String(e));
    }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader title="General" subtitle="App-wide preferences." />
      <Field label="Default save path" help="New torrents land here unless you override per-add in the modal.">
        <div class="flex items-center gap-2">
          <input
            type="text"
            class="flex-1 rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-100 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40"
            value={savePath()}
            onInput={(e) => setSavePath(e.currentTarget.value)}
          />
          <Button variant="primary" onClick={save} disabled={!dirty() || !savePath().trim()}>
            Save
          </Button>
        </div>
      </Field>

      <div class="mt-6 mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">Watch Folder</h2>
        <p class="mt-0.5 text-sm text-zinc-500">
          Automatically add any .torrent file dropped into the watched directory.
        </p>
      </div>

      <Field label="Enable watch folder" help="Poll the configured folder every 5 seconds for new .torrent files.">
        <label class="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            class="h-4 w-4 rounded border-white/10 bg-black/30 text-accent-500 focus:ring-accent-500/30"
            checked={wfEnabled()}
            onChange={(e) => setWfEnabled(e.currentTarget.checked)}
          />
          <span class="text-sm text-zinc-300">Enabled</span>
        </label>
      </Field>

      <Field label="Folder path" help="Path to the directory to watch.">
        <div class="flex items-center gap-2">
          <input
            type="text"
            class="flex-1 rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-100 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40 disabled:opacity-40"
            value={wfPath()}
            onInput={(e) => setWfPath(e.currentTarget.value)}
            disabled={!wfEnabled()}
          />
          {isWailsRuntime() && (
            <Button variant="secondary" onClick={browseWatchFolder} disabled={!wfEnabled()}>
              Browse…
            </Button>
          )}
        </div>
      </Field>

      <Field label="Delete after add" help="Remove the .torrent file from the folder once it has been added.">
        <label class="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            class="h-4 w-4 rounded border-white/10 bg-black/30 text-accent-500 focus:ring-accent-500/30 disabled:opacity-40"
            checked={wfDelete()}
            onChange={(e) => setWfDelete(e.currentTarget.checked)}
            disabled={!wfEnabled()}
          />
          <span class="text-sm text-zinc-300">Delete .torrent file after adding</span>
        </label>
      </Field>

      <div class="mt-4 flex justify-end">
        <Button
          variant="primary"
          onClick={saveWatchFolder}
          disabled={!wfDirty() || (wfEnabled() && !wfPath().trim())}
        >
          Save watch folder
        </Button>
      </div>
    </div>
  );
}
