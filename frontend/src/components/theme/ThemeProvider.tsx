import {createContext, createSignal, createEffect, useContext, type JSX, type Accessor} from 'solid-js';
import {loadStoredTheme, storeTheme, resolveTheme, type Theme, type ResolvedTheme} from '../../lib/theme';

type ThemeContextValue = {
  theme: Accessor<Theme>;
  resolved: Accessor<ResolvedTheme>;
  setTheme: (t: Theme) => void;
};

const ThemeContext = createContext<ThemeContextValue>();

export function ThemeProvider(props: {children: JSX.Element}) {
  const [theme, setThemeSignal] = createSignal<Theme>(loadStoredTheme());

  const mq = window.matchMedia('(prefers-color-scheme: dark)');
  const [systemDark, setSystemDark] = createSignal(mq.matches);
  mq.addEventListener('change', (e) => setSystemDark(e.matches));

  const resolved = () => resolveTheme(theme(), systemDark());

  createEffect(() => {
    const r = resolved();
    document.documentElement.dataset.theme = r;
    document.documentElement.style.colorScheme = r;
  });

  const setTheme = (t: Theme) => {
    setThemeSignal(t);
    storeTheme(t);
  };

  return (
    <ThemeContext.Provider value={{theme, resolved, setTheme}}>
      {props.children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used inside <ThemeProvider>');
  return ctx;
}
