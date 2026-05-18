import {createSignal, createResource, For, Show, type JSX} from 'solid-js';
import {toast} from 'solid-sonner';
import {Check, Copy, Pencil, Trash2, KeyRound, Plus, ShieldCheck, User as UserIcon} from 'lucide-solid';
import {api, type UserDTO, type UserInput, type UserRole} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  // The logged-in account, used for the "Your account" section and to decide
  // whether the admin user-management section renders.
  currentUser: UserDTO | null;
};

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

const inputClass =
  'w-full rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30';

// Permission flags, in display order. Each maps to a UserInput / UserDTO key.
const PERMS: {key: keyof UserInput & string; label: string; help: string}[] = [
  {key: 'perm_add_torrents', label: 'Add torrents', help: 'Add magnets and .torrent files'},
  {key: 'perm_share', label: 'Share torrents', help: 'Share owned torrents with other users'},
  {key: 'perm_manage_rss', label: 'Manage RSS', help: 'Create and edit RSS feeds and filters'},
  {key: 'perm_manage_cat_tags', label: 'Manage categories & tags', help: 'Create, edit and delete the shared category / tag sets'},
  {key: 'perm_change_settings', label: 'Change global settings', help: 'Bandwidth, connection, schedule and blocklist'},
];

function blankInput(): UserInput {
  return {
    username: '',
    password: '',
    role: 'user',
    perm_add_torrents: true,
    perm_manage_rss: false,
    perm_manage_cat_tags: false,
    perm_change_settings: false,
    perm_share: true,
    disabled: false,
  };
}

function inputFromUser(u: UserDTO): UserInput {
  return {
    username: u.username,
    password: '',
    role: u.role,
    perm_add_torrents: u.perm_add_torrents,
    perm_manage_rss: u.perm_manage_rss,
    perm_manage_cat_tags: u.perm_manage_cat_tags,
    perm_change_settings: u.perm_change_settings,
    perm_share: u.perm_share,
    disabled: u.disabled,
  };
}

export function UsersPane(props: Props) {
  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader
        title="Users"
        subtitle="Multi-user accounts. Each user sees only their own torrents plus anything shared with them; all accounts share one engine and run under one OS user."
      />
      <YourAccount currentUser={props.currentUser} />
      <Show when={props.currentUser?.role === 'admin'}>
        <AllUsers currentUserID={props.currentUser?.id ?? 0} />
      </Show>
    </div>
  );
}

// ---- Your account ----

function YourAccount(props: {currentUser: UserDTO | null}) {
  const [oldPw, setOldPw] = createSignal('');
  const [newPw, setNewPw] = createSignal('');
  const [revealedKey, setRevealedKey] = createSignal<string | null>(null);
  const [copied, setCopied] = createSignal(false);

  const changePassword = async () => {
    if (newPw().length < 8) {
      toast.error('New password must be at least 8 characters');
      return;
    }
    try {
      await api.changeMyPassword(oldPw(), newPw());
      setOldPw('');
      setNewPw('');
      toast.success('Password changed — you may need to log in again');
    } catch (err) {
      toast.error(String(err));
    }
  };

  const rotateKey = async () => {
    try {
      const key = await api.rotateMyAPIKey();
      setRevealedKey(key);
      setCopied(false);
    } catch (err) {
      toast.error(String(err));
    }
  };

  const copyKey = async () => {
    const k = revealedKey();
    if (!k) return;
    try {
      await navigator.clipboard.writeText(k);
      setCopied(true);
      toast.success('Copied');
    } catch (err) {
      toast.error(String(err));
    }
  };

  return (
    <section class="mb-8">
      <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-zinc-400">Your account</h3>
      <div class="rounded-lg border border-white/[.06] bg-white/[.01] p-4">
        <div class="mb-3 flex items-center gap-2">
          <UserIcon class="h-4 w-4 text-zinc-400" />
          <span class="text-sm text-zinc-100">{props.currentUser?.username ?? 'admin'}</span>
          <span class="rounded bg-white/[.06] px-1.5 py-0.5 text-xs text-zinc-400">
            {props.currentUser?.role ?? 'admin'}
          </span>
        </div>

        <div class="grid grid-cols-[160px_1fr] items-center gap-3 py-2">
          <div class="text-sm text-zinc-300">Current password</div>
          <input type="password" class={inputClass} value={oldPw()} autocomplete="current-password"
            onInput={(e) => setOldPw(e.currentTarget.value)} />
          <div class="text-sm text-zinc-300">New password</div>
          <input type="password" class={inputClass} value={newPw()} autocomplete="new-password"
            placeholder="At least 8 characters"
            onInput={(e) => setNewPw(e.currentTarget.value)} />
        </div>
        <div class="mt-2 flex justify-end">
          <Button variant="primary" onClick={changePassword} disabled={!newPw() || !oldPw()}>
            Change password
          </Button>
        </div>

        <div class="mt-4 border-t border-white/[.04] pt-3">
          <div class="text-sm text-zinc-200">API key</div>
          <p class="mt-0.5 mb-2 text-xs text-zinc-500">
            Bearer token for programmatic clients. Rotating invalidates the previous key.
            <Show when={props.currentUser?.has_api_key && !revealedKey()}>
              {' '}Current key ends in <code class="text-zinc-400">…{props.currentUser?.api_key_hint}</code>.
            </Show>
          </p>
          <div class="flex flex-col gap-2">
            <div>
              <Button variant="ghost" onClick={rotateKey}>
                <KeyRound class="h-3.5 w-3.5" /> Generate API key
              </Button>
            </div>
            <Show when={revealedKey()}>
              <div class="flex items-center gap-2">
                <code class="flex-1 break-all rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-200">
                  {revealedKey()}
                </code>
                <button type="button" onClick={copyKey} aria-label="Copy API key"
                  class="grid h-7 w-7 place-items-center rounded-md border border-white/[.06] text-zinc-400 hover:bg-white/[.06] hover:text-zinc-100">
                  <Show when={copied()} fallback={<Copy class="h-3.5 w-3.5" />}>
                    <Check class="h-3.5 w-3.5 text-seed" />
                  </Show>
                </button>
              </div>
              <p class="text-xs text-zinc-500">Save this now — it won't be shown again.</p>
            </Show>
          </div>
        </div>
      </div>
    </section>
  );
}

// ---- All users (admin only) ----

function AllUsers(props: {currentUserID: number}) {
  const [users, {refetch}] = createResource(() => api.listUsers());
  // editing holds the user being edited (null = none); creating toggles the
  // new-user form. They are mutually exclusive.
  const [editing, setEditing] = createSignal<UserDTO | null>(null);
  const [creating, setCreating] = createSignal(false);

  const onSaved = () => {
    setEditing(null);
    setCreating(false);
    refetch();
  };

  const remove = async (u: UserDTO) => {
    if (!confirm(`Delete user "${u.username}"? Torrents only they owned stay in the engine but become admin-only.`)) {
      return;
    }
    try {
      await api.deleteUser(u.id);
      toast.success(`Deleted ${u.username}`);
      refetch();
    } catch (err) {
      toast.error(String(err));
    }
  };

  const resetPassword = async (u: UserDTO) => {
    const pw = prompt(`New password for "${u.username}" (min 8 chars):`);
    if (pw === null) return;
    if (pw.length < 8) {
      toast.error('Password must be at least 8 characters');
      return;
    }
    try {
      await api.resetUserPassword(u.id, pw);
      toast.success(`Password reset for ${u.username}`);
    } catch (err) {
      toast.error(String(err));
    }
  };

  return (
    <section>
      <div class="mb-2 flex items-center justify-between">
        <h3 class="text-sm font-semibold uppercase tracking-wide text-zinc-400">All users</h3>
        <Show when={!creating() && !editing()}>
          <Button variant="ghost" onClick={() => setCreating(true)}>
            <Plus class="h-3.5 w-3.5" /> Add user
          </Button>
        </Show>
      </div>

      <Show when={creating()}>
        <UserForm
          title="New user"
          initial={blankInput()}
          requirePassword
          onCancel={() => setCreating(false)}
          onSubmit={async (input) => {
            await api.createUser(input);
            toast.success(`Created ${input.username}`);
            onSaved();
          }}
        />
      </Show>

      <Show when={editing()}>
        {(u) => (
          <UserForm
            title={`Edit ${u().username}`}
            initial={inputFromUser(u())}
            requirePassword={false}
            onCancel={() => setEditing(null)}
            onSubmit={async (input) => {
              await api.updateUser(u().id, input);
              toast.success(`Updated ${input.username}`);
              onSaved();
            }}
          />
        )}
      </Show>

      <Show when={!creating() && !editing()}>
        <ul class="flex flex-col gap-1.5">
          <For each={users()} fallback={<li class="text-sm text-zinc-500">Loading…</li>}>
            {(u) => (
              <li class="flex items-center gap-3 rounded-lg border border-white/[.06] bg-white/[.01] px-3 py-2">
                <div class="grid h-8 w-8 place-items-center rounded-full bg-white/[.04]">
                  <Show when={u.role === 'admin'} fallback={<UserIcon class="h-4 w-4 text-zinc-400" />}>
                    <ShieldCheck class="h-4 w-4 text-accent-400" />
                  </Show>
                </div>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="truncate text-sm text-zinc-100">{u.username}</span>
                    <Show when={u.disabled}>
                      <span class="rounded bg-rose-500/15 px-1.5 py-0.5 text-[10px] uppercase text-rose-300">disabled</span>
                    </Show>
                  </div>
                  <div class="text-xs text-zinc-500">{permSummary(u)}</div>
                </div>
                <button type="button" aria-label="Reset password" onClick={() => resetPassword(u)}
                  class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-white/[.06] hover:text-zinc-100">
                  <KeyRound class="h-3.5 w-3.5" />
                </button>
                <button type="button" aria-label="Edit user" onClick={() => setEditing(u)}
                  class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-white/[.06] hover:text-zinc-100">
                  <Pencil class="h-3.5 w-3.5" />
                </button>
                <button type="button" aria-label="Delete user" onClick={() => remove(u)}
                  disabled={u.id === props.currentUserID || u.id === 1}
                  class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-rose-500/10 hover:text-rose-300 disabled:opacity-30 disabled:hover:bg-transparent">
                  <Trash2 class="h-3.5 w-3.5" />
                </button>
              </li>
            )}
          </For>
        </ul>
      </Show>
    </section>
  );
}

function permSummary(u: UserDTO): string {
  if (u.role === 'admin') return 'Administrator — full access';
  const on = PERMS.filter((p) => u[p.key as keyof UserDTO]).map((p) => p.label.toLowerCase());
  return on.length ? on.join(', ') : 'No permissions';
}

// ---- shared create/edit form ----

function UserForm(props: {
  title: string;
  initial: UserInput;
  requirePassword: boolean;
  onCancel: () => void;
  onSubmit: (input: UserInput) => Promise<void>;
}): JSX.Element {
  const [username, setUsername] = createSignal(props.initial.username);
  const [password, setPassword] = createSignal('');
  const [role, setRole] = createSignal<UserRole>(props.initial.role);
  const [disabled, setDisabled] = createSignal(props.initial.disabled);
  const [perms, setPerms] = createSignal({
    perm_add_torrents: props.initial.perm_add_torrents,
    perm_manage_rss: props.initial.perm_manage_rss,
    perm_manage_cat_tags: props.initial.perm_manage_cat_tags,
    perm_change_settings: props.initial.perm_change_settings,
    perm_share: props.initial.perm_share,
  });
  const [busy, setBusy] = createSignal(false);

  const isAdmin = () => role() === 'admin';

  const submit = async () => {
    if (!username().trim()) {
      toast.error('Username is required');
      return;
    }
    if (props.requirePassword && password().length < 8) {
      toast.error('Password must be at least 8 characters');
      return;
    }
    setBusy(true);
    try {
      await props.onSubmit({
        username: username().trim(),
        password: password() || undefined,
        role: role(),
        disabled: disabled(),
        ...perms(),
      });
    } catch (err) {
      toast.error(String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="mb-3 rounded-lg border border-white/[.06] bg-white/[.02] p-4">
      <div class="mb-3 text-sm font-medium text-zinc-100">{props.title}</div>

      <div class="grid grid-cols-[140px_1fr] items-center gap-3">
        <label class="text-sm text-zinc-300">Username</label>
        <input class={inputClass} value={username()} autocomplete="off"
          onInput={(e) => setUsername(e.currentTarget.value)} />

        <label class="text-sm text-zinc-300">
          {props.requirePassword ? 'Password' : 'Password'}
        </label>
        <input type="password" class={inputClass} value={password()} autocomplete="new-password"
          placeholder={props.requirePassword ? 'At least 8 characters' : 'Leave blank — use “reset password”'}
          disabled={!props.requirePassword}
          onInput={(e) => setPassword(e.currentTarget.value)} />

        <label class="text-sm text-zinc-300">Role</label>
        <select class={`${inputClass} w-40`} value={role()}
          onChange={(e) => setRole(e.currentTarget.value as UserRole)}>
          <option value="user">User</option>
          <option value="admin">Admin</option>
        </select>
      </div>

      <div class="mt-3 border-t border-white/[.04] pt-3">
        <div class="mb-1.5 text-xs uppercase tracking-wide text-zinc-500">Permissions</div>
        <Show when={isAdmin()}>
          <p class="mb-2 text-xs text-zinc-500">Admins hold every permission and can manage users.</p>
        </Show>
        <div class="flex flex-col gap-1.5">
          <For each={PERMS}>
            {(p) => (
              <label class="flex items-start gap-2 text-sm text-zinc-300">
                <input type="checkbox" class="mt-0.5 accent-accent-500"
                  checked={isAdmin() ? true : perms()[p.key as keyof ReturnType<typeof perms>]}
                  disabled={isAdmin()}
                  onChange={(e) => setPerms({...perms(), [p.key]: e.currentTarget.checked})} />
                <span>
                  {p.label}
                  <span class="block text-xs text-zinc-500">{p.help}</span>
                </span>
              </label>
            )}
          </For>
        </div>
      </div>

      <label class="mt-3 flex items-center gap-2 text-sm text-zinc-300">
        <input type="checkbox" class="accent-accent-500" checked={disabled()}
          onChange={(e) => setDisabled(e.currentTarget.checked)} />
        Account disabled (cannot log in)
      </label>

      <div class="mt-4 flex justify-end gap-2">
        <Button variant="ghost" onClick={props.onCancel} disabled={busy()}>Cancel</Button>
        <Button variant="primary" onClick={submit} disabled={busy()}>Save</Button>
      </div>
    </div>
  );
}
