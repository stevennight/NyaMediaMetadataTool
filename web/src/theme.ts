import { setDesktopWindowTheme } from './desktop';

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export const themeStorageKey = 'nya.theme.mode';

function isThemeMode(value: string | null): value is ThemeMode {
  return value === 'system' || value === 'light' || value === 'dark';
}

function systemPrefersDark(): boolean {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}

export function readThemeMode(): ThemeMode {
  try {
    const stored = window.localStorage.getItem(themeStorageKey);
    return isThemeMode(stored) ? stored : 'system';
  } catch {
    return 'system';
  }
}

export function resolveTheme(mode: ThemeMode, prefersDark = systemPrefersDark()): ResolvedTheme {
  if (mode === 'system') return prefersDark ? 'dark' : 'light';
  return mode;
}

function applyDocumentTheme(mode: ThemeMode, resolved: ResolvedTheme) {
  const root = document.documentElement;
  root.dataset.themeMode = mode;
  root.dataset.theme = resolved;
  root.style.colorScheme = resolved;
}

function persistThemeMode(mode: ThemeMode) {
  try {
    window.localStorage.setItem(themeStorageKey, mode);
  } catch {
    // The visual preference still applies for this session when storage is unavailable.
  }
}

export function initializeTheme(): ThemeMode {
  const mode = readThemeMode();
  applyDocumentTheme(mode, resolveTheme(mode));
  return mode;
}

export function applyThemeMode(mode: ThemeMode): () => void {
  persistThemeMode(mode);
  const media = window.matchMedia?.('(prefers-color-scheme: dark)');
  const sync = () => {
    const resolved = resolveTheme(mode, media?.matches ?? false);
    applyDocumentTheme(mode, resolved);
    setDesktopWindowTheme(mode, resolved);
  };

  sync();
  if (mode !== 'system' || !media) return () => undefined;

  if (typeof media.addEventListener === 'function') {
    media.addEventListener('change', sync);
    return () => media.removeEventListener('change', sync);
  }

  media.addListener(sync);
  return () => media.removeListener(sync);
}
