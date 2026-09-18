import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { addResourceOnce, resolveTabId } from './tabmedia';
import type { MediaResource } from './media';

function resource(over: Partial<MediaResource> = {}): MediaResource {
  return {
    id: 'r1',
    url: 'https://cdn.example.com/clip.mp4',
    tabId: 7,
    filename: 'clip.mp4',
    mimeType: 'video/mp4',
    isHls: false,
    foundAt: 1,
    ...over,
  };
}

describe('tabmedia routing', () => {
  it('falls back to the sender tab when the message carries no tabId', () => {
    assert.equal(resolveTabId(undefined, 7), 7);
  });

  it('prefers the explicit tabId sent by the popup', () => {
    assert.equal(resolveTabId(12, 7), 12);
  });

  it('returns null when neither side knows the tab', () => {
    assert.equal(resolveTabId(undefined, undefined), null);
  });
});

describe('tabmedia pool', () => {
  it('adds a new resource and reports the change', () => {
    const list: MediaResource[] = [];

    assert.equal(addResourceOnce(list, resource()), true);
    assert.equal(list.length, 1);
  });

  it('ignores the same url twice so the pool does not grow on reloads', () => {
    const list: MediaResource[] = [resource()];

    assert.equal(addResourceOnce(list, resource({ id: 'r2' })), false);
    assert.equal(list.length, 1);
  });

  it('keeps distinct urls of the same tab apart', () => {
    const list: MediaResource[] = [resource()];

    assert.equal(
      addResourceOnce(list, resource({ id: 'r3', url: 'https://cdn.example.com/other.mp4' })),
      true,
    );
    assert.equal(list.length, 2);
  });
});
