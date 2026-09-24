import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  KEY_MASKS,
  SHORTCUT_GRACE_PERIOD_MS,
  isKeyPressed,
  keyStateReply,
  normalizeKeyName,
  resolveEffectiveKeyMask,
  updateKeyMask,
  urlsMatch,
} from './shortcuts';

describe('shortcuts', () => {
  it('normalizeKeyName handles aliases and case variations', () => {
    assert.equal(normalizeKeyName('ctrl'), 'Control');
    assert.equal(normalizeKeyName('Control'), 'Control');
    assert.equal(normalizeKeyName('shift'), 'Shift');
    assert.equal(normalizeKeyName('Shift'), 'Shift');
    assert.equal(normalizeKeyName('alt'), 'Alt');
    assert.equal(normalizeKeyName('ins'), 'Insert');
    assert.equal(normalizeKeyName('Insert'), 'Insert');
    assert.equal(normalizeKeyName('del'), 'Delete');
    assert.equal(normalizeKeyName('Delete'), 'Delete');
    assert.equal(normalizeKeyName('Enter'), null);
  });

  it('updateKeyMask sets and clears bits cleanly', () => {
    let mask = 0;
    // Press Ctrl
    mask = updateKeyMask(mask, 'Control', true);
    assert.equal(mask, KEY_MASKS.Control);
    assert.equal(isKeyPressed(mask, 'Ctrl'), true);
    assert.equal(isKeyPressed(mask, 'Alt'), false);

    // Press Alt while Ctrl is held
    mask = updateKeyMask(mask, 'Alt', true);
    assert.equal(mask, KEY_MASKS.Control | KEY_MASKS.Alt);
    assert.equal(isKeyPressed(mask, 'Ctrl'), true);
    assert.equal(isKeyPressed(mask, 'Alt'), true);

    // Release Ctrl
    mask = updateKeyMask(mask, 'Control', false);
    assert.equal(mask, KEY_MASKS.Alt);
    assert.equal(isKeyPressed(mask, 'Ctrl'), false);
    assert.equal(isKeyPressed(mask, 'Alt'), true);

    // Release Alt
    mask = updateKeyMask(mask, 'Alt', false);
    assert.equal(mask, 0);
    assert.equal(isKeyPressed(mask, 'Alt'), false);
  });

  it('urlsMatch compares URLs correctly', () => {
    assert.equal(urlsMatch('https://example.com/file.zip', 'https://example.com/file.zip'), true);
    assert.equal(urlsMatch('https://example.com/file.zip/', 'https://example.com/file.zip'), true);
    assert.equal(urlsMatch('https://example.com/file.zip', 'https://example.com/other.zip'), false);
    assert.equal(urlsMatch('https://a.com/file.zip', 'https://b.com/file.zip'), false);
    // Wildcard match when click url is missing
    assert.equal(urlsMatch(undefined, 'https://example.com/file.zip'), true);
  });

  it('resolveEffectiveKeyMask preserves key states within grace period and respects click intent', () => {
    const now = 100000;

    // Case 1: Key is physically held right now
    const held = resolveEffectiveKeyMask(
      KEY_MASKS.Delete,
      0,
      0,
      [],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(held, KEY_MASKS.Delete);

    // Case 2: Key was released 500ms ago (within SHORTCUT_GRACE_PERIOD_MS)
    const releasedRecently = resolveEffectiveKeyMask(
      0,
      KEY_MASKS.Delete,
      now - 500,
      [],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(releasedRecently, KEY_MASKS.Delete);

    // Case 3: Key was released 5000ms ago (expired)
    const releasedLongAgo = resolveEffectiveKeyMask(
      0,
      KEY_MASKS.Delete,
      now - (SHORTCUT_GRACE_PERIOD_MS + 1000),
      [],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(releasedLongAgo, 0);

    // Case 4: Key was released, but user had clicked the exact download link with Delete held 800ms ago
    const clickedMatched = resolveEffectiveKeyMask(
      0,
      0,
      0,
      [{ keyMask: KEY_MASKS.Delete, url: 'https://example.com/file.zip', timestamp: now - 800 }],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(clickedMatched, KEY_MASKS.Delete);

    // Case 5: Click intent with different target url does not match
    const clickedMismatched = resolveEffectiveKeyMask(
      0,
      0,
      0,
      [{ keyMask: KEY_MASKS.Delete, url: 'https://example.com/other.zip', timestamp: now - 800 }],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(clickedMismatched, 0);

    // Case 6: Click intent without specific url (e.g. JS button download) matches within grace period
    const clickedGeneric = resolveEffectiveKeyMask(
      0,
      0,
      0,
      [{ keyMask: KEY_MASKS.Delete, timestamp: now - 1200 }],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(clickedGeneric, KEY_MASKS.Delete);

    // Case 7: Click intent expired after grace period
    const clickedExpired = resolveEffectiveKeyMask(
      0,
      0,
      0,
      [{ keyMask: KEY_MASKS.Delete, timestamp: now - (SHORTCUT_GRACE_PERIOD_MS + 500) }],
      'https://example.com/file.zip',
      now,
    );
    assert.equal(clickedExpired, 0);
  });

  it('keyStateReply 只在真的按住键时应答', () => {
    assert.deepEqual(keyStateReply(KEY_MASKS.Insert | KEY_MASKS.Shift), {
      keyMask: KEY_MASKS.Insert | KEY_MASKS.Shift,
    });
    assert.equal(
      keyStateReply(0),
      undefined,
      '没按住时不应答：回一个 0 会让后台把「没答案」当成答案',
    );
  });
});
