import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  decideTakeover,
  extractExtension,
  extractHostname,
  isExtensionMatched,
  isSiteExcluded,
} from './rules';
import { KEY_MASKS } from './shortcuts';
import type { TakeoverConfigSync } from './types';

describe('rules', () => {
  it('extractExtension parses extensions correctly from URLs and filenames', () => {
    assert.equal(extractExtension('archive.zip'), 'zip');
    assert.equal(extractExtension('archive.tar.gz'), 'gz');
    assert.equal(extractExtension('https://example.com/downloads/file.7z?token=abc#hash'), '7z');
    assert.equal(extractExtension('no_extension'), '');
    assert.equal(extractExtension('.hiddenfile'), '');
    assert.equal(extractExtension('https://example.com/path/'), '');
  });

  it('isExtensionMatched handles case-insensitivity and dot prefixes', () => {
    const list = ['zip', 'rar', '.7z', 'MP4'];
    assert.equal(isExtensionMatched('test.zip', list), true);
    assert.equal(isExtensionMatched('test.ZIP', list), true);
    assert.equal(isExtensionMatched('test.7z', list), true);
    assert.equal(isExtensionMatched('test.mp4', list), true);
    assert.equal(isExtensionMatched('test.pdf', list), false);
  });

  it('extractHostname parses domains cleanly', () => {
    assert.equal(extractHostname('https://sub.domain.com:8080/path'), 'sub.domain.com');
    assert.equal(extractHostname('github.com'), 'github.com');
    assert.equal(extractHostname(''), '');
  });

  it('isSiteExcluded matches exact, subdomains, and wildcards', () => {
    const excluded = ['github.com', '*.bank.example.com', '*cdn*'];

    // 1. Exact match and subdomains
    assert.equal(isSiteExcluded('https://github.com/repo', excluded), true);
    assert.equal(isSiteExcluded('https://api.github.com/v1', excluded), true);
    assert.equal(isSiteExcluded('https://notgithub.com', excluded), false);

    // 2. Wildcard prefix
    assert.equal(isSiteExcluded('https://bank.example.com', excluded), true);
    assert.equal(isSiteExcluded('https://secure.bank.example.com', excluded), true);
    assert.equal(isSiteExcluded('https://other.example.com', excluded), false);

    // 3. General wildcard
    assert.equal(isSiteExcluded('https://mycdn-server.net', excluded), true);
    assert.equal(isSiteExcluded('https://fastcdn.org', excluded), true);
    assert.equal(isSiteExcluded('https://normal.org', excluded), false);
  });

  it('decideTakeover prioritizes pause and force shortcuts correctly', () => {
    const config: TakeoverConfigSync = {
      version: 1,
      extensions: ['zip', 'mp4'],
      excludedSites: ['excluded.com'],
      pauseShortcut: 'Delete',
      forceShortcut: 'Insert',
    };

    // Case 1: Normal extension match -> takeover
    assert.deepEqual(
      decideTakeover('https://example.com/file.zip', 'file.zip', 'https://example.com', config, 0),
      { takeover: true, reason: 'extension_matched' },
    );

    // Case 2: Unmatched extension -> no takeover
    assert.deepEqual(
      decideTakeover('https://example.com/doc.pdf', 'doc.pdf', 'https://example.com', config, 0),
      { takeover: false, reason: 'extension_not_matched' },
    );

    // Case 3: Excluded site -> no takeover
    assert.deepEqual(
      decideTakeover(
        'https://example.com/file.zip',
        'file.zip',
        'https://excluded.com/page',
        config,
        0,
      ),
      { takeover: false, reason: 'site_excluded' },
    );

    // Case 4: Pause shortcut (Delete) is pressed -> bypasses takeover even for matched extension
    assert.deepEqual(
      decideTakeover(
        'https://example.com/file.zip',
        'file.zip',
        'https://example.com',
        config,
        KEY_MASKS.Delete,
      ),
      { takeover: false, reason: 'pause_shortcut_active' },
    );

    // Case 5: Force shortcut (Insert) is pressed -> forces takeover even for excluded site & non-matching extension
    assert.deepEqual(
      decideTakeover(
        'https://example.com/doc.pdf',
        'doc.pdf',
        'https://excluded.com/page',
        config,
        KEY_MASKS.Insert,
      ),
      { takeover: true, reason: 'force_shortcut_active' },
    );
  });
});
