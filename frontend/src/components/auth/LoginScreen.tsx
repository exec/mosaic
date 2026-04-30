import {createSignal} from 'solid-js';
import {LogIn} from 'lucide-solid';
import {api} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {onLoggedIn: () => void};

export function LoginScreen(props: Props) {
  const [username, setUsername] = createSignal('admin');
  const [password, setPassword] = createSignal('');
  const [error, setError] = createSignal('');
  const [submitting, setSubmitting] = createSignal(false);

  const submit = async (e: Event) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await api.login(username(), password());
      props.onLoggedIn();
    } catch (err) {
      setError(String(err).replace(/^Error:\s*/, '') || 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div class="grid h-full place-items-center px-4">
      <form
        onSubmit={submit}
        class="w-full max-w-sm rounded-lg border border-white/[.06] bg-white/[.02] p-6 shadow-2xl backdrop-blur-md"
      >
        <div class="mb-5 text-center">
          <h1 class="text-lg font-semibold text-zinc-100">Mosaic</h1>
          <p class="mt-1 text-xs text-zinc-500">Sign in to continue.</p>
        </div>

        <label class="mb-3 block">
          <span class="mb-1 block text-xs uppercase tracking-wider text-zinc-500">Username</span>
          <input
            type="text"
            value={username()}
            onInput={(e) => setUsername(e.currentTarget.value)}
            autocomplete="username"
            required
            class="w-full rounded-md border border-white/[.06] bg-white/[.04] px-3 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30"
          />
        </label>

        <label class="mb-4 block">
          <span class="mb-1 block text-xs uppercase tracking-wider text-zinc-500">Password</span>
          <input
            type="password"
            value={password()}
            onInput={(e) => setPassword(e.currentTarget.value)}
            autocomplete="current-password"
            required
            class="w-full rounded-md border border-white/[.06] bg-white/[.04] px-3 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30"
          />
        </label>

        {error() && (
          <div class="mb-3 rounded-md border border-rose-500/20 bg-rose-500/10 px-3 py-1.5 text-xs text-rose-300">
            {error()}
          </div>
        )}

        <Button type="submit" variant="primary" disabled={submitting()} class="w-full justify-center">
          <LogIn class="h-3.5 w-3.5" />
          {submitting() ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
    </div>
  );
}
