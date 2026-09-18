import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { computeBarPosition, MEDIA_BAR_MARGIN, MEDIA_BAR_VIEWPORT_PADDING } from './mediabar';

interface Box {
  top: number;
  right: number;
  bottom: number;
  left: number;
  width: number;
  height: number;
}

const VIEWPORT = { width: 1024, height: 800 };

function box(over: Partial<Box> = {}): Box {
  return { top: 100, right: 800, bottom: 500, left: 200, width: 600, height: 400, ...over };
}

describe('mediabar positioning logic', () => {
  it('anchors the bar to the player top-right corner', () => {
    const pos = computeBarPosition(box(), VIEWPORT, 150);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, 800 - 150 - MEDIA_BAR_MARGIN);
    assert.equal(pos.top, 100 + MEDIA_BAR_MARGIN);
  });

  it('hides the bar when the player is too small to host it', () => {
    assert.equal(computeBarPosition(box({ right: 280, width: 80 }), VIEWPORT, 150).visible, false);
    assert.equal(
      computeBarPosition(box({ bottom: 140, height: 40 }), VIEWPORT, 150).visible,
      false,
    );
  });

  it('hides the bar when the player scrolled out of the viewport', () => {
    assert.equal(
      computeBarPosition(box({ top: -500, bottom: -100 }), VIEWPORT, 150).visible,
      false,
    );
    assert.equal(computeBarPosition(box({ top: 900, bottom: 1300 }), VIEWPORT, 150).visible, false);
  });

  it('keeps the bar inside the viewport when the player overflows to the right', () => {
    const pos = computeBarPosition(box({ left: 0, right: 1200, width: 1200 }), VIEWPORT, 150);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, VIEWPORT.width - 150 - MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 + MEDIA_BAR_MARGIN);
  });

  it('clamps the top edge when the player is scrolled above the viewport', () => {
    const pos = computeBarPosition(box({ top: -50, bottom: 350 }), VIEWPORT, 150);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, MEDIA_BAR_VIEWPORT_PADDING);
  });

  it('never leaves the viewport on the left when the player is scrolled away', () => {
    const pos = computeBarPosition(box({ left: -200, right: 100, width: 300 }), VIEWPORT, 150);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
  });

  it('keeps a bar wider than the visible player on the left edge', () => {
    const pos = computeBarPosition(box({ left: 0, right: 200, width: 200 }), VIEWPORT, 250);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
  });
});
