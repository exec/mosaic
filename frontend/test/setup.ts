// Vitest 2 + jsdom 29 + vite-plugin-solid don't expose a working `localStorage`
// global (it surfaces as a plain proxy without Storage's prototype methods).
// Replace it on `window` with a real Storage-shaped implementation so plan-spec
// tests like `localStorage.clear()` / `getItem` / `setItem` work.
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
