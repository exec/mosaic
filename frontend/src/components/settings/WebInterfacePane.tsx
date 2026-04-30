import {createEffect, createSignal, Show} from 'solid-js';
import {toast} from 'solid-sonner';
import {Check, Copy} from 'lucide-solid';
import type {WebConfigDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  webConfig: WebConfigDTO;
  onSetWebConfig: (c: WebConfigDTO) => Promise<void>;
  onSetWebPassword: (plain: string) => Promise<void>;
  onRotateAPIKey: () => Promise<string>;
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

const inputClass =
  'w-full rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30';

export function WebInterfacePane(props: Props) {
  const [enabled, setEnabled] = createSignal(props.webConfig.enabled);
  const [port, setPort] = createSignal(props.webConfig.port || 8080);
  const [bindAll, setBindAll] = createSignal(props.webConfig.bind_all);
  const [username, setUsername] = createSignal(props.webConfig.username);
  const [password, setPassword] = createSignal('');
  // We never display webConfig.api_key — only keys minted via "Generate" this
  // session, so the user is reminded to copy them somewhere safe.
  const [revealedKey, setRevealedKey] = createSignal<string | null>(null);
  const [keyCopied, setKeyCopied] = createSignal(false);

  // Re-sync when prop changes (initial fetch races, or restart updates).
  createEffect(() => { setEnabled(props.webConfig.enabled); });
  createEffect(() => { setPort(props.webConfig.port || 8080); });
  createEffect(() => { setBindAll(props.webConfig.bind_all); });
  createEffect(() => { setUsername(props.webConfig.username); });

  const dirty = () =>
    enabled() !== props.webConfig.enabled ||
    port() !== props.webConfig.port ||
    bindAll() !== props.webConfig.bind_all ||
    username() !== props.webConfig.username ||
    password().length > 0;

  const portValid = () => port() >= 1024 && port() <= 65535;

  const discard = () => {
    setEnabled(props.webConfig.enabled);
    setPort(props.webConfig.port || 8080);
    setBindAll(props.webConfig.bind_all);
    setUsername(props.webConfig.username);
    setPassword('');
  };

  const save = async () => {
    if (!portValid()) {
      toast.error('Port must be between 1024 and 65535');
      return;
    }
    try {
      await props.onSetWebConfig({
        enabled: enabled(),
        port: port(),
        bind_all: bindAll(),
        username: username(),
        api_key: props.webConfig.api_key,
      });
      if (password().length > 0) {
        await props.onSetWebPassword(password());
        setPassword('');
      }
      toast.success('Web interface settings saved');
    } catch (err) {
      toast.error(String(err));
    }
  };

  const generateKey = async () => {
    try {
      const key = await props.onRotateAPIKey();
      setRevealedKey(key);
      setKeyCopied(false);
    } catch (err) {
      toast.error(String(err));
    }
  };

  const copyKey = async () => {
    const key = revealedKey();
    if (!key) return;
    try {
      await navigator.clipboard.writeText(key);
      setKeyCopied(true);
      toast.success('Copied');
    } catch (err) {
      toast.error(String(err));
    }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader
        title="Web Interface"
        subtitle="Optional HTTPS+WS server so you can use Mosaic from any browser on your network."
      />

      <Field label="Enabled" help="Start the embedded web server now and on every launch.">
        <label class="inline-flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={enabled()}
            onChange={(e) => setEnabled(e.currentTarget.checked)}
            class="accent-accent-500"
            data-testid="web-enabled"
          />
          <span class="text-xs text-zinc-400">{enabled() ? 'On' : 'Off'}</span>
        </label>
      </Field>

      <Field label="Port" help="Range 1024–65535.">
        <input
          type="number"
          min={1024}
          max={65535}
          value={port()}
          onInput={(e) => setPort(parseInt(e.currentTarget.value || '0', 10))}
          class={`${inputClass} w-32 font-mono tabular-nums`}
          data-testid="web-port"
        />
      </Field>

      <Field label="Bind to" help="Loopback restricts access to this machine. All-interfaces switches on HTTPS.">
        <div class="flex flex-col gap-1.5">
          <label class="inline-flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
            <input
              type="radio"
              name="bind"
              checked={!bindAll()}
              onChange={() => setBindAll(false)}
              class="accent-accent-500"
              data-testid="web-bind-localhost"
            />
            Localhost only
          </label>
          <label class="inline-flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
            <input
              type="radio"
              name="bind"
              checked={bindAll()}
              onChange={() => setBindAll(true)}
              class="accent-accent-500"
              data-testid="web-bind-all"
            />
            All interfaces (HTTPS)
          </label>
          <Show when={bindAll()}>
            <p class="text-xs text-zinc-500">Cert is self-signed; browsers will warn the first time.</p>
          </Show>
        </div>
      </Field>

      <Field label="Username">
        <input
          type="text"
          value={username()}
          onInput={(e) => setUsername(e.currentTarget.value)}
          class={inputClass}
          autocomplete="off"
          data-testid="web-username"
        />
      </Field>

      <Field label="Password" help="Stored hashed (Argon2id). Empty = keep current.">
        <input
          type="password"
          value={password()}
          onInput={(e) => setPassword(e.currentTarget.value)}
          placeholder="Leave blank to keep current"
          class={inputClass}
          autocomplete="new-password"
          data-testid="web-password"
        />
      </Field>

      <Field label="API key" help="Bearer token for programmatic clients. Rotating invalidates the previous key.">
        <div class="flex flex-col gap-2">
          <Button variant="ghost" onClick={generateKey} data-testid="web-rotate-key">
            Generate API key
          </Button>
          <Show when={revealedKey()}>
            <div class="flex items-center gap-2">
              <code
                class="flex-1 break-all rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-200"
                data-testid="web-revealed-key"
              >
                {revealedKey()}
              </code>
              <button
                type="button"
                onClick={copyKey}
                class="grid h-7 w-7 place-items-center rounded-md border border-white/[.06] text-zinc-400 hover:bg-white/[.06] hover:text-zinc-100"
                aria-label="Copy API key"
              >
                <Show when={keyCopied()} fallback={<Copy class="h-3.5 w-3.5" />}>
                  <Check class="h-3.5 w-3.5 text-seed" />
                </Show>
              </button>
            </div>
            <p class="text-xs text-zinc-500">Save this now — it won't be shown again.</p>
          </Show>
        </div>
      </Field>

      <div class="flex justify-end gap-2 mt-3">
        <Button variant="ghost" onClick={discard} disabled={!dirty()}>
          Discard
        </Button>
        <Button variant="primary" onClick={save} disabled={!dirty() || !portValid() || !username().trim()}>
          Save
        </Button>
      </div>
    </div>
  );
}
