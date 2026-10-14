import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { injectContentScriptsIntoExistingTabs } from './reinject';

describe('injectContentScriptsIntoExistingTabs', () => {
  it('returns zeroes gracefully when APIs are missing', async () => {
    const res = await injectContentScriptsIntoExistingTabs(undefined as unknown as typeof chrome);
    assert.deepEqual(res, { successTabs: 0, failedTabs: 0 });

    const partialApi = {
      tabs: {},
      scripting: {},
    } as unknown as typeof chrome;
    const res2 = await injectContentScriptsIntoExistingTabs(partialApi);
    assert.deepEqual(res2, { successTabs: 0, failedTabs: 0 });
  });

  it('returns zeroes when manifest has no content scripts', async () => {
    const mockChrome = {
      runtime: {
        getManifest: () => ({ name: 'Test', version: '1.0.0', manifest_version: 3 }),
      },
      tabs: {
        query: async () => [
          { id: 1, url: 'https://example.com' },
          { id: 2, url: 'https://foo.bar' },
        ],
      },
      scripting: {
        insertCSS: async () => {},
        executeScript: async () => {},
      },
    } as unknown as typeof chrome;

    const res = await injectContentScriptsIntoExistingTabs(mockChrome);
    assert.deepEqual(res, { successTabs: 0, failedTabs: 0 });
  });

  it('injects scripts and CSS into queried tabs including custom ports', async () => {
    const insertedCSS: Array<{ tabId: number; allFrames: boolean; files: string[] }> = [];
    const executedScripts: Array<{ tabId: number; allFrames: boolean; files: string[] }> = [];

    const mockChrome = {
      runtime: {
        getManifest: () => ({
          name: 'Test',
          version: '1.0.0',
          manifest_version: 3,
          content_scripts: [
            {
              matches: ['*://*/*'],
              all_frames: true,
              css: ['content.css'],
              js: ['content.js'],
            },
          ],
        }),
      },
      tabs: {
        query: async () => [
          { id: 101, url: 'https://example.com:8443/video' },
          { id: 102, url: 'http://localhost:3000/' },
          { id: 103, url: 'chrome://extensions' }, // 应当被过滤掉
        ],
      },
      scripting: {
        insertCSS: async (opts: {
          target: { tabId: number; allFrames?: boolean };
          files?: string[];
        }) => {
          insertedCSS.push({
            tabId: opts.target.tabId,
            allFrames: opts.target.allFrames ?? false,
            files: opts.files || [],
          });
        },
        executeScript: async (opts: {
          target: { tabId: number; allFrames?: boolean };
          files?: string[];
        }) => {
          executedScripts.push({
            tabId: opts.target.tabId,
            allFrames: opts.target.allFrames ?? false,
            files: opts.files || [],
          });
        },
      },
    } as unknown as typeof chrome;

    const res = await injectContentScriptsIntoExistingTabs(mockChrome);
    assert.deepEqual(res, { successTabs: 2, failedTabs: 0 });

    assert.equal(insertedCSS.length, 2);
    assert.equal(executedScripts.length, 2);

    assert.deepEqual(insertedCSS[0], { tabId: 101, allFrames: true, files: ['content.css'] });
    assert.deepEqual(executedScripts[0], { tabId: 101, allFrames: true, files: ['content.js'] });

    assert.deepEqual(insertedCSS[1], { tabId: 102, allFrames: true, files: ['content.css'] });
    assert.deepEqual(executedScripts[1], { tabId: 102, allFrames: true, files: ['content.js'] });
  });

  it('falls back to top-level frame injection if allFrames injection throws (e.g. restricted iframe)', async () => {
    const executedTargets: Array<{ tabId: number; allFrames?: boolean }> = [];

    const mockChrome = {
      runtime: {
        getManifest: () => ({
          name: 'Test',
          version: '1.0.0',
          manifest_version: 3,
          content_scripts: [
            {
              matches: ['*://*/*'],
              all_frames: true,
              js: ['content.js'],
            },
          ],
        }),
      },
      tabs: {
        query: async () => [{ id: 201, url: 'https://video-site-with-ads.com' }],
      },
      scripting: {
        executeScript: async (opts: { target: { tabId: number; allFrames?: boolean } }) => {
          executedTargets.push(opts.target);
          if (opts.target.allFrames) {
            throw new Error('Cannot access contents of restricted iframe');
          }
        },
      },
    } as unknown as typeof chrome;

    const res = await injectContentScriptsIntoExistingTabs(mockChrome);
    assert.deepEqual(res, { successTabs: 1, failedTabs: 0 });

    // 第一次尝试 allFrames: true 失败，第二次降级为不传 allFrames（仅顶层 frame）
    assert.equal(executedTargets.length, 2);
    assert.deepEqual(executedTargets[0], { tabId: 201, allFrames: true });
    assert.deepEqual(executedTargets[1], { tabId: 201 });
  });
});
