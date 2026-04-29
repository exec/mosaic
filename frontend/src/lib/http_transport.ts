// HTTP+WS transport: routes the Wails-style method names to the chi REST
// endpoints in backend/remote, and demuxes a single shared WebSocket onto
// per-event handlers. Selected by transport.ts when running outside Wails.

import type {Transport} from './transport';

interface RouteSpec {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE';
  /** Builds the URL path from positional args. */
  path: (args: any[]) => string;
  /** Builds the JSON body, or undefined for no body. */
  body?: (args: any[]) => any;
  /** Optionally unwrap the JSON response into the value Wails would return. */
  unwrap?: (raw: any) => any;
  /** Special-case multipart upload. */
  multipart?: (args: any[]) => FormData;
}

const okEnvelope = () => undefined;
const idString = (raw: any) => raw.id as string;
const idNumber = (raw: any) => raw.id as number;

const ROUTES: Record<string, RouteSpec> = {
  // Auth
  Login: {
    method: 'POST',
    path: () => '/api/login',
    body: ([username, password]) => ({username, password}),
    unwrap: okEnvelope,
  },
  Logout: {method: 'POST', path: () => '/api/logout', unwrap: okEnvelope},

  // Torrents
  ListTorrents: {method: 'GET', path: () => '/api/torrents'},
  AddMagnet: {
    method: 'POST',
    path: () => '/api/torrents/magnet',
    body: ([magnet, savePath]) => ({magnet, save_path: savePath}),
    unwrap: idString,
  },
  AddTorrentBytes: {
    method: 'POST',
    path: () => '/api/torrents/file',
    multipart: ([bytes, savePath]) => {
      const buf = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
      const blob = new Blob([buf as BlobPart], {type: 'application/octet-stream'});
      const fd = new FormData();
      fd.append('file', blob, 'upload.torrent');
      fd.append('save_path', savePath ?? '');
      return fd;
    },
    unwrap: idString,
  },
  Pause: {method: 'POST', path: ([id]) => `/api/torrents/${encodeURIComponent(id)}/pause`, unwrap: okEnvelope},
  Resume: {method: 'POST', path: ([id]) => `/api/torrents/${encodeURIComponent(id)}/resume`, unwrap: okEnvelope},
  Remove: {
    method: 'DELETE',
    path: ([id, deleteFiles]) => `/api/torrents/${encodeURIComponent(id)}${deleteFiles ? '?delete=1' : ''}`,
    unwrap: okEnvelope,
  },
  SetTorrentCategory: {
    method: 'POST',
    path: () => '/api/torrents/category',
    body: ([infohash, categoryID]) => ({infohash, category_id: categoryID}),
    unwrap: okEnvelope,
  },
  SetFilePriorities: {
    method: 'POST',
    path: () => '/api/torrents/file_priorities',
    body: ([infohash, prios]) => ({infohash, priorities: prios}),
    unwrap: okEnvelope,
  },
  SetQueuePosition: {
    method: 'POST',
    path: () => '/api/torrents/queue_position',
    body: ([infohash, pos]) => ({infohash, pos}),
    unwrap: okEnvelope,
  },
  SetForceStart: {
    method: 'POST',
    path: () => '/api/torrents/force_start',
    body: ([infohash, force]) => ({infohash, force}),
    unwrap: okEnvelope,
  },

  // Stats / inspector
  GlobalStats: {method: 'GET', path: () => '/api/stats'},
  SetInspectorFocus: {
    method: 'POST',
    path: () => '/api/inspector/focus',
    body: ([id, tabs]) => ({id, tabs}),
    unwrap: okEnvelope,
  },
  ClearInspectorFocus: {method: 'POST', path: () => '/api/inspector/clear', unwrap: okEnvelope},

  // Categories / tags
  ListCategories: {method: 'GET', path: () => '/api/categories'},
  CreateCategory: {
    method: 'POST',
    path: () => '/api/categories',
    body: ([name, savePath, color]) => ({name, default_save_path: savePath, color}),
    unwrap: idNumber,
  },
  UpdateCategory: {
    method: 'PUT',
    path: () => '/api/categories',
    body: ([id, name, savePath, color]) => ({id, name, default_save_path: savePath, color}),
    unwrap: okEnvelope,
  },
  DeleteCategory: {method: 'DELETE', path: ([id]) => `/api/categories/${id}`, unwrap: okEnvelope},
  ListTags: {method: 'GET', path: () => '/api/tags'},
  CreateTag: {
    method: 'POST',
    path: () => '/api/tags',
    body: ([name, color]) => ({name, color}),
    unwrap: idNumber,
  },
  DeleteTag: {method: 'DELETE', path: ([id]) => `/api/tags/${id}`, unwrap: okEnvelope},
  AssignTag: {
    method: 'POST',
    path: () => '/api/tags/assign',
    body: ([infohash, tagID]) => ({infohash, tag_id: tagID}),
    unwrap: okEnvelope,
  },
  UnassignTag: {
    method: 'POST',
    path: () => '/api/tags/unassign',
    body: ([infohash, tagID]) => ({infohash, tag_id: tagID}),
    unwrap: okEnvelope,
  },

  // Settings
  GetDefaultSavePath: {method: 'GET', path: () => '/api/settings/save_path', unwrap: (r) => r.path as string},
  SetDefaultSavePath: {
    method: 'PUT',
    path: () => '/api/settings/save_path',
    body: ([path]) => ({path}),
    unwrap: okEnvelope,
  },
  GetLimits: {method: 'GET', path: () => '/api/settings/limits'},
  SetLimits: {
    method: 'PUT',
    path: () => '/api/settings/limits',
    body: ([l]) => l,
    unwrap: okEnvelope,
  },
  ToggleAltSpeed: {
    method: 'POST',
    path: () => '/api/settings/alt_speed/toggle',
    unwrap: (r) => r.alt_active as boolean,
  },
  GetQueueLimits: {method: 'GET', path: () => '/api/settings/queue_limits'},
  SetQueueLimits: {
    method: 'PUT',
    path: () => '/api/settings/queue_limits',
    body: ([q]) => q,
    unwrap: okEnvelope,
  },
  GetBlocklist: {method: 'GET', path: () => '/api/settings/blocklist'},
  SetBlocklistURL: {
    method: 'PUT',
    path: () => '/api/settings/blocklist',
    body: ([url, enabled]) => ({url, enabled}),
    unwrap: okEnvelope,
  },
  RefreshBlocklist: {method: 'POST', path: () => '/api/settings/blocklist/refresh', unwrap: okEnvelope},
  GetWebConfig: {method: 'GET', path: () => '/api/settings/web'},
  SetWebConfig: {
    method: 'PUT',
    path: () => '/api/settings/web',
    body: ([c]) => c,
    unwrap: okEnvelope,
  },
  SetWebPassword: {
    method: 'PUT',
    path: () => '/api/settings/web/password',
    body: ([plain]) => ({password: plain}),
    unwrap: okEnvelope,
  },
  RotateAPIKey: {
    method: 'POST',
    path: () => '/api/settings/web/api_key/rotate',
    unwrap: (r) => r.api_key as string,
  },

  // Schedule
  ListScheduleRules: {method: 'GET', path: () => '/api/schedule_rules'},
  CreateScheduleRule: {
    method: 'POST',
    path: () => '/api/schedule_rules',
    body: ([r]) => r,
    unwrap: idNumber,
  },
  UpdateScheduleRule: {
    method: 'PUT',
    path: () => '/api/schedule_rules',
    body: ([r]) => r,
    unwrap: okEnvelope,
  },
  DeleteScheduleRule: {method: 'DELETE', path: ([id]) => `/api/schedule_rules/${id}`, unwrap: okEnvelope},

  // RSS
  ListFeeds: {method: 'GET', path: () => '/api/feeds'},
  CreateFeed: {
    method: 'POST',
    path: () => '/api/feeds',
    body: ([f]) => f,
    unwrap: idNumber,
  },
  UpdateFeed: {
    method: 'PUT',
    path: () => '/api/feeds',
    body: ([f]) => f,
    unwrap: okEnvelope,
  },
  DeleteFeed: {method: 'DELETE', path: ([id]) => `/api/feeds/${id}`, unwrap: okEnvelope},
  ListFiltersByFeed: {method: 'GET', path: ([feedID]) => `/api/feeds/${feedID}/filters`},
  CreateFilter: {
    method: 'POST',
    path: () => '/api/filters',
    body: ([f]) => f,
    unwrap: idNumber,
  },
  UpdateFilter: {
    method: 'PUT',
    path: () => '/api/filters',
    body: ([f]) => f,
    unwrap: okEnvelope,
  },
  DeleteFilter: {method: 'DELETE', path: ([id]) => `/api/filters/${id}`, unwrap: okEnvelope},
};

interface FetchOptions {
  fetchImpl?: typeof fetch;
  wsCtor?: new (url: string) => WebSocket;
}

export function makeHTTPTransport(origin: string, opts: FetchOptions = {}): Transport {
  const fetchImpl = opts.fetchImpl ?? (typeof fetch !== 'undefined' ? fetch.bind(globalThis) : undefined);
  const wsCtor = opts.wsCtor ?? (typeof WebSocket !== 'undefined' ? WebSocket : undefined);

  const handlers = new Map<string, Set<(data: any) => void>>();
  let socket: WebSocket | null = null;

  function ensureSocket() {
    if (socket || !wsCtor) return;
    const wsURL = origin.replace(/^http/, 'ws') + '/api/ws';
    socket = new wsCtor(wsURL);
    socket.onmessage = (ev) => {
      try {
        const env = JSON.parse(ev.data);
        const subs = handlers.get(env.type);
        if (!subs) return;
        for (const h of subs) h(env.payload);
      } catch (err) {
        console.error('ws message decode', err);
      }
    };
    socket.onclose = () => {
      socket = null;
      // Reconnect after a short delay if any handlers are still subscribed.
      if (handlers.size > 0) {
        setTimeout(ensureSocket, 1000);
      }
    };
    socket.onerror = (e) => console.error('ws error', e);
  }

  return {
    async invoke<T>(method: string, ...args: any[]): Promise<T> {
      const route = ROUTES[method];
      if (!route) {
        throw new Error(`HTTP transport has no route for: ${method}`);
      }
      if (!fetchImpl) {
        throw new Error('fetch is not available in this environment');
      }
      const url = origin + route.path(args);
      const init: RequestInit = {
        method: route.method,
        credentials: 'include',
      };
      if (route.multipart) {
        init.body = route.multipart(args);
        // Browser sets Content-Type with boundary automatically.
      } else if (route.body) {
        init.body = JSON.stringify(route.body(args));
        init.headers = {'Content-Type': 'application/json'};
      }
      const resp = await fetchImpl(url, init);
      const text = await resp.text();
      const parsed = text ? safeJSON(text) : undefined;
      if (!resp.ok) {
        const msg = parsed && parsed.error ? parsed.error : `HTTP ${resp.status}`;
        throw new Error(msg);
      }
      return (route.unwrap ? route.unwrap(parsed) : parsed) as T;
    },
    on(event, handler) {
      let set = handlers.get(event);
      if (!set) {
        set = new Set();
        handlers.set(event, set);
      }
      set.add(handler);
      ensureSocket();
      return () => {
        const s = handlers.get(event);
        if (s) {
          s.delete(handler);
          if (s.size === 0) handlers.delete(event);
        }
        if (handlers.size === 0 && socket) {
          socket.close();
          socket = null;
        }
      };
    },
  };
}

function safeJSON(text: string): any {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}
