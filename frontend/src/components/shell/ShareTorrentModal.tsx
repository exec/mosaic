import {Dialog} from '@kobalte/core/dialog';
import {Share2, X, Trash2} from 'lucide-solid';
import {createEffect, createSignal, For, Show} from 'solid-js';
import {toast} from 'solid-sonner';
import {Button} from '../ui/Button';
import {api, type ShareDTO, type TorrentAccess, type UserDTO} from '../../lib/bindings';

type Props = {
  open: boolean;
  torrentID: string | null;
  torrentName: string;
  // The logged-in user — excluded from the "share with" list (you can't share
  // a torrent with yourself).
  currentUserID: number;
  onClose: () => void;
};

const inputClass =
  'rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 text-sm text-zinc-200 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40';

export function ShareTorrentModal(props: Props) {
  const [users, setUsers] = createSignal<UserDTO[]>([]);
  const [shares, setShares] = createSignal<ShareDTO[]>([]);
  const [pickUser, setPickUser] = createSignal<number | null>(null);
  const [pickAccess, setPickAccess] = createSignal<TorrentAccess>('viewer');
  const [busy, setBusy] = createSignal(false);

  const refresh = async () => {
    const id = props.torrentID;
    if (!id) return;
    try {
      const [u, s] = await Promise.all([api.listUsers(), api.listTorrentShares(id)]);
      if (props.torrentID !== id) return; // stale — dialog moved to another torrent mid-flight
      setUsers(u);
      setShares(s);
    } catch (err) {
      toast.error(String(err));
    }
  };

  createEffect(() => {
    if (props.open && props.torrentID) {
      setPickUser(null);
      setPickAccess('viewer');
      void refresh();
    }
  });

  // Users who already have access (owner or an existing share) are not
  // offered again in the picker.
  const shareableUsers = () => {
    const taken = new Set(shares().map((s) => s.user_id));
    return users().filter((u) => u.id !== props.currentUserID && !taken.has(u.id) && !u.disabled);
  };

  const addShare = async () => {
    const id = props.torrentID;
    const uid = pickUser();
    if (!id || uid === null) return;
    setBusy(true);
    try {
      await api.shareTorrent(id, uid, pickAccess());
      toast.success('Shared');
      setPickUser(null);
      await refresh();
    } catch (err) {
      toast.error(String(err));
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (userID: number) => {
    const id = props.torrentID;
    if (!id) return;
    try {
      await api.unshareTorrent(id, userID);
      await refresh();
    } catch (err) {
      toast.error(String(err));
    }
  };

  return (
    <Dialog open={props.open} onOpenChange={(o) => { if (!o) props.onClose(); }}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm animate-in fade-in" />
        <div class="fixed inset-0 z-50 grid place-items-center p-4">
          <Dialog.Content class="w-full max-w-lg rounded-xl border border-white/10 bg-zinc-900/95 backdrop-blur-xl shadow-2xl modal-in">
            <div class="flex flex-col gap-3 p-5">
              <div class="flex items-center justify-between">
                <Dialog.Title class="inline-flex items-center gap-2 text-base font-semibold text-zinc-100">
                  <Share2 class="h-4 w-4 text-accent-500" />
                  Share torrent
                </Dialog.Title>
                <Dialog.CloseButton class="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-white/[.06] hover:text-zinc-100">
                  <X class="h-4 w-4" />
                </Dialog.CloseButton>
              </div>

              <p class="truncate text-sm text-zinc-400" title={props.torrentName}>{props.torrentName}</p>

              {/* Current access grants */}
              <div class="rounded-lg border border-white/[.06] bg-white/[.01]">
                <For
                  each={shares()}
                  fallback={<div class="px-3 py-2 text-sm text-zinc-500">No access grants yet.</div>}
                >
                  {(s) => (
                    <div class="flex items-center gap-2 border-b border-white/[.03] px-3 py-2 last:border-b-0">
                      <span class="flex-1 truncate text-sm text-zinc-200">{s.username}</span>
                      <span class="rounded bg-white/[.06] px-1.5 py-0.5 text-xs text-zinc-400">{s.access}</span>
                      <Show when={s.access !== 'owner'}>
                        <button type="button" aria-label="Revoke access" onClick={() => revoke(s.user_id)}
                          class="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-rose-500/10 hover:text-rose-300">
                          <Trash2 class="h-3.5 w-3.5" />
                        </button>
                      </Show>
                    </div>
                  )}
                </For>
              </div>

              {/* Add a new share */}
              <div class="flex items-end gap-2">
                <label class="flex flex-1 flex-col gap-1">
                  <span class="text-xs text-zinc-500">User</span>
                  <select
                    class={inputClass}
                    value={pickUser() ?? ''}
                    onChange={(e) => setPickUser(e.currentTarget.value ? Number(e.currentTarget.value) : null)}
                  >
                    <option value="">Select a user…</option>
                    <For each={shareableUsers()}>
                      {(u) => <option value={u.id}>{u.username}</option>}
                    </For>
                  </select>
                </label>
                <label class="flex flex-col gap-1">
                  <span class="text-xs text-zinc-500">Access</span>
                  <select
                    class={inputClass}
                    value={pickAccess()}
                    onChange={(e) => setPickAccess(e.currentTarget.value as TorrentAccess)}
                  >
                    <option value="viewer">Viewer (read-only)</option>
                    <option value="editor">Editor (can control)</option>
                  </select>
                </label>
                <Button variant="primary" onClick={addShare} disabled={busy() || pickUser() === null}>
                  Share
                </Button>
              </div>
              <Show when={shareableUsers().length === 0}>
                <p class="text-xs text-zinc-500">No other users available to share with.</p>
              </Show>
            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
