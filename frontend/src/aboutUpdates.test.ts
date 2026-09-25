import { describe, it, expect } from 'vitest';
import * as updateModels from '../bindings/sheep-get/internal/update/models';

describe('update models and contracts', () => {
  it('instantiates AppUpdateResult correctly for portable mode', () => {
    const res = new updateModels.AppUpdateResult({
      hasUpdate: true,
      currentVersion: '1.0.0',
      latestVersion: '1.1.0',
      releaseTitle: 'SheepGet 1.1.0',
      releaseNotes: 'Performance improvements',
      releaseUrl: 'https://github.com/ygq-future/SheepGet/releases/tag/v1.1.0',
      assetName: 'SheepGet_1.1.0_windows-x64-portable.zip',
      assetUrl: 'https://github.com/downloads/port.zip',
      assetSize: 10485760,
      isPortable: true,
    });

    expect(res.hasUpdate).toBe(true);
    expect(res.isPortable).toBe(true);
    expect(res.latestVersion).toBe('1.1.0');
    expect(res.assetName).toContain('portable');
  });

  it('instantiates AppUpdateResult correctly for setup mode', () => {
    const res = new updateModels.AppUpdateResult({
      hasUpdate: true,
      currentVersion: '1.0.0',
      latestVersion: '1.1.0',
      releaseTitle: 'SheepGet 1.1.0',
      assetName: 'SheepGet_1.1.0_x64-setup.exe',
      assetUrl: 'https://github.com/downloads/setup.exe',
      assetSize: 20971520,
      isPortable: false,
    });

    expect(res.hasUpdate).toBe(true);
    expect(res.isPortable).toBe(false);
    expect(res.assetName).toContain('setup.exe');
  });

  it('instantiates ExtensionUpdateResult for decoupled extension updating', () => {
    const res = new updateModels.ExtensionUpdateResult({
      hasUpdate: true,
      currentVersion: '1.0.0',
      latestVersion: '1.2.0',
      releaseTitle: 'Extension update',
      assetName: 'SheepGet_1.2.0_extension-chrome-mv3.zip',
      assetUrl: 'https://github.com/downloads/ext.zip',
      assetSize: 524288,
    });

    expect(res.hasUpdate).toBe(true);
    expect(res.latestVersion).toBe('1.2.0');
    expect(res.assetName).toContain('extension');
  });

  it('instantiates DownloadProgress correctly', () => {
    const progress = new updateModels.DownloadProgress({
      downloadedBytes: 5242880,
      totalBytes: 10485760,
      percentage: 50.0,
      speedBps: 1048576,
    });

    expect(progress.percentage).toBe(50.0);
    expect(progress.downloadedBytes).toBe(5242880);
    expect(progress.speedBps).toBe(1048576);
  });
});
