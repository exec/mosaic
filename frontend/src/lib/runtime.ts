// Single source of truth for "are we running inside the Wails desktop shell?"
// vs "are we running in a plain browser hitting the remote HTTPS+WS interface?"
// Used to gate native-only code paths (e.g. Wails OpenFileDialog) so the same
// SolidJS app can run in both shells.
export const isWailsRuntime = (): boolean =>
  typeof window !== 'undefined' && !!(window as any).runtime;
