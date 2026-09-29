import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { decideTakeover } from './rules';
import { KEY_MASKS } from './shortcuts';
import type { TakeoverConfigSync } from './types';

const testConfig: TakeoverConfigSync = {
  version: 1,
  extensions: ['zip', 'exe', '7z'],
  excludedSites: ['excluded.example.com'],
  pauseShortcut: 'Delete',
  forceShortcut: 'Insert',
};

describe('网页端直接点击超前拦截判定', () => {
  it('命中接管后缀且无快捷键时，应判定接管并在前端拦截点击（消除原生下载动画）', () => {
    const decision = decideTakeover(
      'https://example.com/downloads/package.zip',
      undefined,
      'https://example.com/page',
      testConfig,
      0,
    );
    assert.equal(decision.takeover, true);
    assert.equal(decision.reason, 'extension_matched');
  });

  it('按住 Delete 键时，不应接管，放行浏览器原生下载（完整保留原生动画与气泡）', () => {
    const decision = decideTakeover(
      'https://example.com/downloads/package.zip',
      undefined,
      'https://example.com/page',
      testConfig,
      KEY_MASKS.Delete,
    );
    assert.equal(decision.takeover, false);
    assert.equal(decision.reason, 'pause_shortcut_active');
  });

  it('命中排除站点时，不应接管，放行浏览器原生下载', () => {
    const decision = decideTakeover(
      'https://example.com/downloads/package.zip',
      undefined,
      'https://excluded.example.com/page',
      testConfig,
      0,
    );
    assert.equal(decision.takeover, false);
    assert.equal(decision.reason, 'site_excluded');
  });

  it('未命中接管后缀的文件，不应接管，放行浏览器原生下载', () => {
    const decision = decideTakeover(
      'https://example.com/docs/manual.pdf',
      undefined,
      'https://example.com/page',
      testConfig,
      0,
    );
    assert.equal(decision.takeover, false);
    assert.equal(decision.reason, 'extension_not_matched');
  });

  it('未命中接管后缀但按住 Insert 强制键时，应强制接管', () => {
    const decision = decideTakeover(
      'https://example.com/docs/manual.pdf',
      undefined,
      'https://example.com/page',
      testConfig,
      KEY_MASKS.Insert,
    );
    assert.equal(decision.takeover, true);
    assert.equal(decision.reason, 'force_shortcut_active');
  });
});
