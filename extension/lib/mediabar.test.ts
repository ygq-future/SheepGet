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
const BAR = { width: 150, height: 26 };

function box(over: Partial<Box> = {}): Box {
  return { top: 100, right: 800, bottom: 500, left: 200, width: 600, height: 400, ...over };
}

describe('mediabar positioning logic', () => {
  it('anchors the bar above the player, right-aligned to its right edge', () => {
    const pos = computeBarPosition(box(), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    // 右对齐：悬浮条右边缘与播放器右边缘对齐
    assert.equal(pos.left, 800 - BAR.width);
    // 在播放器上方，整条都在画面之外
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
    assert.ok(pos.top + BAR.height <= 100, 'bar must not cover the player');
  });

  it('drops below the player when there is no room above, still not covering it', () => {
    const player = box({ top: 4, bottom: 404 });
    const pos = computeBarPosition(player, VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, 800 - BAR.width);
    assert.equal(pos.top, 404 + MEDIA_BAR_MARGIN);
    assert.ok(pos.top >= player.bottom, 'bar must not cover the player');
  });

  it('falls back inside the viewport when the player leaves no room either side', () => {
    // 占满整个视口的播放器：上下都没有外侧位置可放
    const pos = computeBarPosition(box({ top: -120, bottom: 880, height: 1000 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.left, 800 - BAR.width);
  });

  it('hides the bar when the player is too small to host it', () => {
    assert.equal(computeBarPosition(box({ right: 280, width: 80 }), VIEWPORT, BAR).visible, false);
    assert.equal(
      computeBarPosition(box({ bottom: 140, height: 40 }), VIEWPORT, BAR).visible,
      false,
    );
  });

  it('hides the bar when the player scrolled out of the viewport', () => {
    assert.equal(
      computeBarPosition(box({ top: -500, bottom: -100 }), VIEWPORT, BAR).visible,
      false,
    );
    assert.equal(computeBarPosition(box({ top: 900, bottom: 1300 }), VIEWPORT, BAR).visible, false);
  });

  it('keeps the bar inside the viewport when the player overflows to the right', () => {
    const pos = computeBarPosition(box({ left: 0, right: 1200, width: 1200 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, VIEWPORT.width - BAR.width - MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
  });

  it('clamps the top edge when the player is scrolled above the viewport', () => {
    const pos = computeBarPosition(box({ top: -50, bottom: 350 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, 350 + MEDIA_BAR_MARGIN);
  });

  it('never leaves the viewport on the left when the player is scrolled away', () => {
    const pos = computeBarPosition(box({ left: -200, right: 100, width: 300 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
  });

  it('keeps a bar wider than the visible player on the left edge', () => {
    const pos = computeBarPosition(box({ left: 0, right: 200, width: 200 }), VIEWPORT, {
      width: 250,
      height: 26,
    });

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
  });
});
