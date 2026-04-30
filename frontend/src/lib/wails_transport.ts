// Wails IPC transport: thin adapter that forwards to the auto-generated
// bindings on `window.go.main.App`, and pipes Wails events through
// `window.runtime.EventsOn` (the runtime is injected by Wails at startup;
// we don't import from the generated wailsjs/ directory because that path
// is gitignored and absent in CI).

import type {Transport} from './transport';

export function makeWailsTransport(): Transport {
  return {
    invoke<T>(method: string, ...args: any[]): Promise<T> {
      const w = window as any;
      const fn = w?.go?.main?.App?.[method];
      if (typeof fn !== 'function') {
        return Promise.reject(new Error(`Wails binding not found: ${method}`));
      }
      // Wails IPC marshals []byte as a JS number[] — convert at this boundary
      // so callers can pass a clean Uint8Array.
      const marshalled = args.map((a) => (a instanceof Uint8Array ? Array.from(a) : a));
      return fn(...marshalled) as Promise<T>;
    },
    on(event, handler) {
      const w = window as any;
      const eventsOn = w?.runtime?.EventsOn;
      if (typeof eventsOn !== 'function') return () => {};
      // Wails returns its own off-fn. Wrap to a stable () => void.
      const off = eventsOn(event, handler) as unknown as () => void;
      return off ?? (() => {});
    },
  };
}
