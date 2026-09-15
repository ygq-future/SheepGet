import { create } from 'zustand';
import { GetSettings, UpdateSettings, GetStorageInfo } from '../../bindings/sheep-get/app';
import * as configModels from '../../bindings/sheep-get/internal/config/models';
import { Events } from '@wailsio/runtime';
import { unwrapEventData } from '../lib/utils';

export interface StorageInfo {
  mode: string;
  dataDir: string;
}

interface SettingsState {
  settings: configModels.Settings | null;
  storageInfo: StorageInfo | null;
  isLoading: boolean;
  error: string | null;

  loadSettings: () => Promise<void>;
  updateSettings: (newSettings: Partial<configModels.Settings>) => Promise<void>;
  applyThemeAndAccent: (settings: configModels.Settings) => void;
}

export function hexToRgb(hex: string): { r: number; g: number; b: number } | null {
  const cleanHex = hex.replace('#', '').trim();
  if (cleanHex.length === 3) {
    const r = parseInt(cleanHex[0] + cleanHex[0], 16);
    const g = parseInt(cleanHex[1] + cleanHex[1], 16);
    const b = parseInt(cleanHex[2] + cleanHex[2], 16);
    return isNaN(r) || isNaN(g) || isNaN(b) ? null : { r, g, b };
  }
  if (cleanHex.length === 6) {
    const r = parseInt(cleanHex.substring(0, 2), 16);
    const g = parseInt(cleanHex.substring(2, 4), 16);
    const b = parseInt(cleanHex.substring(4, 6), 16);
    return isNaN(r) || isNaN(g) || isNaN(b) ? null : { r, g, b };
  }
  return null;
}

export function injectThemeAndAccentCSS(settings: configModels.Settings) {
  const root = document.documentElement;
  const theme = settings.appearance?.theme || configModels.ThemeMode.ThemeSystem;
  const accent = settings.appearance?.accentColor || '#10b981';

  try {
    localStorage.setItem('sheepget_theme', theme);
  } catch {
    // Ignore storage errors in restricted contexts
  }
  // Apply dark / light class to root
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const isDark =
    theme === configModels.ThemeMode.ThemeDark ||
    (theme === configModels.ThemeMode.ThemeSystem && prefersDark);

  const isFileInfo =
    root.classList.contains('window-fileinfo') ||
    new URLSearchParams(window.location.search).get('window') === 'fileinfo';

  const bgApp = isDark ? '#0c0e12' : '#f1f5f9';
  root.style.setProperty('--bg-app', bgApp);
  if (!isFileInfo) {
    root.style.backgroundColor = bgApp;
    if (document.body) {
      document.body.style.backgroundColor = bgApp;
    }
  } else {
    root.style.backgroundColor = 'transparent';
    if (document.body) {
      document.body.style.backgroundColor = 'transparent';
    }
  }

  if (isDark) {
    root.classList.add('dark');
    root.classList.remove('light');
    root.style.setProperty('--bg-base', '#09090b');
    root.style.setProperty('--bg-surface', '#121215');
    root.style.setProperty('--bg-surface-hover', '#18181c');
    root.style.setProperty('--bg-subtle', '#1e1e24');
    root.style.setProperty('--text-primary', '#f4f4f5');
    root.style.setProperty('--text-secondary', '#a1a1aa');
    root.style.setProperty('--text-muted', '#71717a');
    root.style.setProperty('--border-subtle', 'rgba(255, 255, 255, 0.06)');
  } else {
    root.classList.add('light');
    root.classList.remove('dark');
    root.style.setProperty('--bg-base', '#f8fafc');
    root.style.setProperty('--bg-surface', '#ffffff');
    root.style.setProperty('--bg-surface-hover', '#f1f5f9');
    root.style.setProperty('--bg-subtle', '#e2e8f0');
    root.style.setProperty('--text-primary', '#0f172a');
    root.style.setProperty('--text-secondary', '#475569');
    root.style.setProperty('--text-muted', '#64748b');
    root.style.setProperty('--border-subtle', 'rgba(0, 0, 0, 0.08)');
  }

  // Accent color variables
  root.style.setProperty('--accent', accent);
  const rgb = hexToRgb(accent);
  if (rgb) {
    root.style.setProperty('--accent-muted', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.15)`);
    root.style.setProperty('--border-focus', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.5)`);
  }
}

export const useSettingsStore = create<SettingsState>((set, get) => ({
  settings: null,
  storageInfo: null,
  isLoading: false,
  error: null,

  applyThemeAndAccent: (settings: configModels.Settings) => {
    injectThemeAndAccentCSS(settings);
  },

  loadSettings: async () => {
    set({ isLoading: true, error: null });
    try {
      const [settings, storage] = await Promise.all([GetSettings(), GetStorageInfo()]);
      set({
        settings,
        storageInfo: {
          mode: storage.mode || 'installed',
          dataDir: storage.dataDir || '',
        },
        isLoading: false,
      });
      get().applyThemeAndAccent(settings);
    } catch (err) {
      set({ error: String(err), isLoading: false });
    }
  },

  updateSettings: async (partialSettings: Partial<configModels.Settings>) => {
    const current = get().settings;
    if (!current) return;

    // Merge settings
    const merged = new configModels.Settings({
      appearance: new configModels.AppearanceConfig({
        ...current.appearance,
        ...partialSettings.appearance,
      }),
      download: new configModels.DownloadConfig({
        ...current.download,
        ...partialSettings.download,
      }),
    });

    try {
      const saved = await UpdateSettings(merged);
      set({ settings: saved });
      get().applyThemeAndAccent(saved);
    } catch (err) {
      set({ error: String(err) });
      throw err;
    }
  },
}));

// Listen to settings:updated events
export function initSettingsListener() {
  return Events.On('settings:updated', (event: unknown) => {
    const updated = unwrapEventData<configModels.Settings>(event);
    if (!updated) return;
    useSettingsStore.setState({ settings: updated });
    useSettingsStore.getState().applyThemeAndAccent(updated);
  });
}
