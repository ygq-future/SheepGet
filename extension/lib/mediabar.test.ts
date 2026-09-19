import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  computeBarPosition,
  MEDIA_BAR_INNER_PADDING,
  MEDIA_BAR_VIEWPORT_PADDING,
} from './mediabar';

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
  it('floats the bar inside the top-right corner of the player', () => {
    const pos = computeBarPosition(box(), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    // 右缘贴播放器右缘、上缘贴播放器顶缘，各留一小段内侧边距
    assert.equal(pos.left, 800 - BAR.width - MEDIA_BAR_INNER_PADDING);
    assert.equal(pos.top, 100 + MEDIA_BAR_INNER_PADDING);
    // 整条悬浮条都落在播放器画面范围内
    assert.ok(pos.left >= 200, 'bar must stay inside the player horizontally');
    assert.ok(pos.top >= 100, 'bar must stay inside the player vertically');
  });

  it('sticks to the visible top when the player is scrolled above the viewport', () => {
    const pos = computeBarPosition(box({ top: -50, bottom: 350 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    // 播放器顶部在视口外：按可见部分（视口顶）的右上角定位
    assert.equal(pos.top, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.left, 800 - BAR.width - MEDIA_BAR_INNER_PADDING);
  });

  it('keeps the bar inside the viewport when the player overflows to the right', () => {
    const pos = computeBarPosition(box({ left: 0, right: 1200, width: 1200 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, VIEWPORT.width - BAR.width - MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 + MEDIA_BAR_INNER_PADDING);
  });

  it('anchors to the viewport corner when the player fills the whole viewport', () => {
    const pos = computeBarPosition(box({ top: -120, bottom: 880, height: 1000 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, MEDIA_BAR_INNER_PADDING);
    assert.equal(pos.left, 800 - BAR.width - MEDIA_BAR_INNER_PADDING);
  });

  it('hides the bar when the player is too small to host it', () => {
    assert.equal(computeBarPosition(box({ right: 280, width: 80 }), VIEWPORT, BAR).visible, false);
    assert.equal(
      computeBarPosition(box({ bottom: 140, height: 40 }), VIEWPORT, BAR).visible,
      false,
    );
  });

  it('hides the bar when no part of the player is visible', () => {
    assert.equal(
      computeBarPosition(box({ top: -500, bottom: -100 }), VIEWPORT, BAR).visible,
      false,
    );
    assert.equal(computeBarPosition(box({ top: 900, bottom: 1300 }), VIEWPORT, BAR).visible, false);
  });

  it('keeps the bar on the viewport edge when the player is scrolled away to the left', () => {
    const pos = computeBarPosition(box({ left: -200, right: 100, width: 300 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    // 可见部分只剩 100px：左缘兜底到视口 padding，不让悬浮条滑出屏幕
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 + MEDIA_BAR_INNER_PADDING);
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
