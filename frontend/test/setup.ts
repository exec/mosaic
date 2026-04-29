// Vitest 2 + vite-plugin-solid don't expose a working `localStorage` global —
// neither under jsdom nor happy-dom. Standalone happy-dom has a real Storage
// instance on its Window, but vitest's environment shim exposes only a plain
// Object stub without Storage's prototype methods (clear/setItem/getItem are
// undefined). Replace it on `window`/`globalThis` with a Storage-shaped
// implementation so plan-spec tests like `localStorage.clear()` work.
class MemoryStorage implements Storage {
  private data = new Map<string, string>();
  get length(): number { return this.data.size; }
  clear(): void { this.data.clear(); }
  getItem(key: string): string | null { return this.data.has(key) ? this.data.get(key)! : null; }
  key(index: number): string | null { return Array.from(this.data.keys())[index] ?? null; }
  removeItem(key: string): void { this.data.delete(key); }
  setItem(key: string, value: string): void { this.data.set(key, String(value)); }
}

const storage = new MemoryStorage();
Object.defineProperty(window, 'localStorage', {value: storage, writable: false, configurable: true});
Object.defineProperty(globalThis, 'localStorage', {value: storage, writable: false, configurable: true});
