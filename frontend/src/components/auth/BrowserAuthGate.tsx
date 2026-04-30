import {createSignal, onMount, Match, Switch, type JSX} from 'solid-js';
import {LoginScreen} from './LoginScreen';

type AuthState = 'checking' | 'login' | 'ok';

type Props = {children: JSX.Element};

// Probes a gated endpoint to decide whether the SPA needs to log in.
// Wails desktop bypasses this entirely (no auth needed).
export function BrowserAuthGate(props: Props) {
  const [state, setState] = createSignal<AuthState>('checking');

  const probe = async () => {
    try {
      const r = await fetch('/api/torrents', {credentials: 'include'});
      if (r.ok) {
        setState('ok');
      } else if (r.status === 401) {
        setState('login');
      } else {
        // Some other error (5xx, etc.) — surface as login so the user can retry.
        setState('login');
      }
    } catch {
      // Network unreachable. Show login so we at least have something.
      setState('login');
    }
  };

  onMount(probe);

  return (
    <Switch>
      <Match when={state() === 'checking'}>
        <div class="grid h-full place-items-center text-xs text-zinc-500">Loading…</div>
      </Match>
      <Match when={state() === 'login'}>
        <LoginScreen onLoggedIn={() => setState('ok')} />
      </Match>
      <Match when={state() === 'ok'}>{props.children}</Match>
    </Switch>
  );
}
