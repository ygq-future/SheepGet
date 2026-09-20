import { describe, it, expect } from 'vitest';
import { hexToRgb } from './stores/settings';
import * as configModels from '../bindings/sheep-get/internal/config/models';

describe('settings store utilities', () => {
  it('converts 3-digit hex color to RGB correctly', () => {
    expect(hexToRgb('#fff')).toEqual({ r: 255, g: 255, b: 255 });
    expect(hexToRgb('#000')).toEqual({ r: 0, g: 0, b: 0 });
    expect(hexToRgb('#f00')).toEqual({ r: 255, g: 0, b: 0 });
  });

  it('converts 6-digit hex color to RGB correctly', () => {
    expect(hexToRgb('#10b981')).toEqual({ r: 16, g: 185, b: 129 });
    expect(hexToRgb('#3b82f6')).toEqual({ r: 59, g: 130, b: 246 });
    expect(hexToRgb('#000000')).toEqual({ r: 0, g: 0, b: 0 });
  });

  it('rejects invalid hex formats', () => {
    expect(hexToRgb('invalid')).toBeNull();
    expect(hexToRgb('#12345')).toBeNull();
    expect(hexToRgb('#1234567')).toBeNull();
    expect(hexToRgb('')).toBeNull();
  });
});

describe('settings models and defaults', () => {
  it('instantiates DownloadConfig with category rules and server file time', () => {
    const download = new configModels.DownloadConfig({
      defaultDirectory: '/test/downloads',
      tempDirectory: '/test/temp',
      useServerFileTime: true,
      builtinCategories: [
        new configModels.CategoryConfig({
          id: 'builtin-video',
          name: '视频',
          directory: '/test/downloads/Videos',
          extensions: ['mp4', 'mkv'],
          isBuiltin: true,
        }),
      ],
      customCategories: [
        new configModels.CategoryConfig({
          id: 'custom-1',
          name: '工作文档',
          directory: '/test/work',
          extensions: ['docx', 'xlsx'],
          isBuiltin: false,
        }),
      ],
    });

    expect(download.defaultDirectory).toBe('/test/downloads');
    expect(download.tempDirectory).toBe('/test/temp');
    expect(download.useServerFileTime).toBe(true);
    expect(download.builtinCategories).toHaveLength(1);
    expect(download.customCategories).toHaveLength(1);
    expect(download.customCategories[0].name).toBe('工作文档');
  });

  it('instantiates General, Proxy, Takeover, and Clipboard configs correctly', () => {
    const general = new configModels.GeneralConfig({ launchAtStartup: true });
    expect(general.launchAtStartup).toBe(true);

    const proxy = new configModels.ProxyConfig({
      mode: configModels.ProxyMode.ProxyModeCustom,
      customAddr: 'http://127.0.0.1:7890',
    });
    expect(proxy.mode).toBe(configModels.ProxyMode.ProxyModeCustom);
    expect(proxy.customAddr).toBe('http://127.0.0.1:7890');

    const takeover = new configModels.TakeoverConfig({
      excludedSites: ['github.com', 'example.org'],
      pauseShortcut: 'Alt',
      forceShortcut: 'Ctrl',
    });
    expect(takeover.excludedSites).toEqual(['github.com', 'example.org']);
    expect(takeover.pauseShortcut).toBe('Alt');
    expect(takeover.forceShortcut).toBe('Ctrl');

    const clipboard = new configModels.ClipboardConfig({ enabled: true });
    expect(clipboard.enabled).toBe(true);

    const root = new configModels.Settings({
      general,
      proxy,
      takeover,
      clipboard,
      appearance: new configModels.AppearanceConfig(),
      download: new configModels.DownloadConfig(),
    });
    expect(root.general.launchAtStartup).toBe(true);
    expect(root.proxy.mode).toBe(configModels.ProxyMode.ProxyModeCustom);
    expect(root.clipboard.enabled).toBe(true);
  });
});
