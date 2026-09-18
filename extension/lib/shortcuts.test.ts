import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { KEY_MASKS, isKeyPressed, normalizeKeyName, updateKeyMask } from './shortcuts';

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
});
