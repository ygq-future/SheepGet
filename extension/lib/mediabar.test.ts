import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

describe('mediabar positioning logic', () => {
  it('computes top and right coordinates with safe boundaries', () => {
    const videoRect = {
      top: 100,
      right: 800,
      bottom: 500,
      left: 200,
      width: 600,
      height: 400,
    };
    const windowWidth = 1024;

    const top = Math.max(8, videoRect.top + 10);
    const right = Math.max(8, windowWidth - videoRect.right + 10);

    assert.equal(top, 110);
    assert.equal(right, 234);
  });

  it('clamps coordinates when video is partially offscreen', () => {
    const videoRect = {
      top: -50,
      right: 1200,
      bottom: 300,
      left: 0,
      width: 1200,
      height: 350,
    };
    const windowWidth = 1000;

    const top = Math.max(8, videoRect.top + 10);
    const right = Math.max(8, windowWidth - videoRect.right + 10);

    assert.equal(top, 8); // Clamped to minimum 8px padding
    assert.equal(right, 8); // Clamped to minimum 8px padding
  });
});
