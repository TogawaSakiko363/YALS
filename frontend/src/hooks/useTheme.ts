import { useCallback, useEffect, useState } from 'react';

export type ThemeMode = 'light' | 'dark' | 'system';
type Resolved = 'light' | 'dark';

const STORAGE_KEY = 'yals-theme';

function systemPrefersDark(): boolean {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function readMode(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === 'light' || v === 'dark' || v === 'system') return v;
  } catch {
    // ignore (private mode / unavailable storage)
  }
  return 'system';
}

function resolve(mode: ThemeMode): Resolved {
  if (mode === 'system') return systemPrefersDark() ? 'dark' : 'light';
  return mode;
}

function apply(resolved: Resolved) {
  document.documentElement.dataset.theme = resolved;
}

// useTheme manages the light/dark theme. The mode is persisted; 'system' follows
// the OS and live-updates when it changes. The resolved theme is written to
// <html data-theme> (the pre-paint script in index.html sets the initial value).
export function useTheme() {
  const [mode, setModeState] = useState<ThemeMode>(() => readMode());
  const [resolved, setResolved] = useState<Resolved>(() => resolve(readMode()));

  // Keep <html data-theme> and the resolved value in sync with the mode, and
  // follow the OS while in 'system' mode.
  useEffect(() => {
    const update = () => {
      const r = resolve(mode);
      setResolved(r);
      apply(r);
    };
    update();
    if (mode !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  }, [mode]);

  const setMode = useCallback((next: ThemeMode) => {
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // ignore
    }
    setModeState(next);
  }, []);

  // Toggle flips between explicit light and dark (resolving 'system' first).
  const toggle = useCallback(() => {
    setMode(resolve(mode) === 'dark' ? 'light' : 'dark');
  }, [mode, setMode]);

  return { mode, resolved, setMode, toggle };
}
