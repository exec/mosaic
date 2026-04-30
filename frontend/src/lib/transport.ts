// Transport abstracts how `bindings.ts` talks to the backend. In a Wails
// shell we go through the auto-generated IPC bindings; in a plain browser
// (the optional HTTPS+WS interface) we go through the JSON+WS adapter at
// /api/*. Everything above this layer is identical.

export interface Transport {
  /** Invoke a backend method by its Wails-style name. */
  invoke<T>(method: string, ...args: any[]): Promise<T>;
  /** Subscribe to an event (`torrents:tick` etc); returns an unsubscriber. */
  on(event: string, handler: (data: any) => void): () => void;
}

import {makeWailsTransport} from './wails_transport';
import {makeHTTPTransport} from './http_transport';

function isWailsRuntime(): boolean {
  return typeof window !== 'undefined' && (window as any).runtime !== undefined;
}

export const transport: Transport = isWailsRuntime()
  ? makeWailsTransport()
  : makeHTTPTransport(typeof window !== 'undefined' ? window.location.origin : '');
