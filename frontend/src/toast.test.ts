import { describe, it, expect } from 'vitest';
import {
  getToastStyle,
  VISIBLE_STACK_COUNT,
  STACK_OFFSET_Y,
  STACK_SCALE_FACTOR,
  STACK_BASE_Z_INDEX,
  getToastHeight,
  TOAST_HEIGHT_SINGLE,
  TOAST_HEIGHT_TITLED,
  DEFAULT_TOAST_DURATIONS,
} from './components/ui/Toast';

describe('Toast layout calculation', () => {
  it('calculates top active card correctly when collapsed', () => {
    const style = getToastStyle(0, false, 0);
    expect(style.y).toBe(0);
    expect(style.scale).toBe(1);
    expect(style.opacity).toBe(1);
    expect(style.zIndex).toBe(STACK_BASE_Z_INDEX);
    expect(style.pointerEvents).toBe('auto');
  });

  it('calculates stacked background cards correctly when collapsed', () => {
    const depth1 = getToastStyle(1, false, 60);
    expect(depth1.y).toBe(-STACK_OFFSET_Y);
    expect(depth1.scale).toBe(1 - STACK_SCALE_FACTOR);
    expect(depth1.opacity).toBeCloseTo(0.82, 2);
    expect(depth1.zIndex).toBe(STACK_BASE_Z_INDEX - 1);
    expect(depth1.pointerEvents).toBe('none');

    const depth2 = getToastStyle(2, false, 120);
    expect(depth2.y).toBe(-2 * STACK_OFFSET_Y);
    expect(depth2.scale).toBe(1 - 2 * STACK_SCALE_FACTOR);
    expect(depth2.opacity).toBeCloseTo(0.64, 2);
    expect(depth2.zIndex).toBe(STACK_BASE_Z_INDEX - 2);
    expect(depth2.pointerEvents).toBe('none');
  });

  it('hides cards exceeding VISIBLE_STACK_COUNT when collapsed', () => {
    const depth3 = getToastStyle(3, false, 180);
    expect(depth3.opacity).toBe(0);
    expect(depth3.zIndex).toBe(0);
    expect(depth3.pointerEvents).toBe('none');
    expect(depth3.y).toBe(-VISIBLE_STACK_COUNT * STACK_OFFSET_Y);

    const depth5 = getToastStyle(5, false, 300);
    expect(depth5.opacity).toBe(0);
    expect(depth5.zIndex).toBe(0);
    expect(depth5.pointerEvents).toBe('none');
  });

  it('expands all cards vertically when hovered', () => {
    const depth0 = getToastStyle(0, true, 0);
    expect(depth0.y).toBe(0);
    expect(depth0.scale).toBe(1);
    expect(depth0.opacity).toBe(1);
    expect(depth0.pointerEvents).toBe('auto');

    const depth1 = getToastStyle(1, true, 64);
    expect(depth1.y).toBe(-64);
    expect(depth1.scale).toBe(1);
    expect(depth1.opacity).toBe(1);
    expect(depth1.pointerEvents).toBe('auto');

    const depth2 = getToastStyle(2, true, 136);
    expect(depth2.y).toBe(-136);
    expect(depth2.scale).toBe(1);
    expect(depth2.opacity).toBe(1);
    expect(depth2.pointerEvents).toBe('auto');
  });

  it('maintains proper z-index ordering when hovered to ensure top cards remain over bottom cards', () => {
    const depth0 = getToastStyle(0, true, 0);
    const depth1 = getToastStyle(1, true, 64);
    const depth2 = getToastStyle(2, true, 136);
    expect(depth0.zIndex).toBeGreaterThan(depth1.zIndex);
    expect(depth1.zIndex).toBeGreaterThan(depth2.zIndex);
  });

  it('calculates estimated heights correctly for single-line vs titled toasts', () => {
    expect(getToastHeight({ id: '1', type: 'info', message: 'Hello', createdAt: Date.now() })).toBe(
      TOAST_HEIGHT_SINGLE,
    );
    expect(
      getToastHeight({
        id: '2',
        type: 'error',
        title: 'Error Title',
        message: 'Hello',
        createdAt: Date.now(),
      }),
    ).toBe(TOAST_HEIGHT_TITLED);
  });

  it('verifies category-specific durations: info <= success < warning < error', () => {
    expect(DEFAULT_TOAST_DURATIONS.info).toBe(2000);
    expect(DEFAULT_TOAST_DURATIONS.success).toBe(2000);
    expect(DEFAULT_TOAST_DURATIONS.warning).toBe(3000);
    expect(DEFAULT_TOAST_DURATIONS.error).toBe(3500);

    expect(DEFAULT_TOAST_DURATIONS.info).toBeLessThanOrEqual(DEFAULT_TOAST_DURATIONS.success);
    expect(DEFAULT_TOAST_DURATIONS.success).toBeLessThan(DEFAULT_TOAST_DURATIONS.warning);
    expect(DEFAULT_TOAST_DURATIONS.warning).toBeLessThan(DEFAULT_TOAST_DURATIONS.error);
  });

  it('verifies category durations match the strict specification', () => {
    expect(DEFAULT_TOAST_DURATIONS.info).toBe(2000);
    expect(DEFAULT_TOAST_DURATIONS.success).toBe(2000);
    expect(DEFAULT_TOAST_DURATIONS.warning).toBe(3000);
    expect(DEFAULT_TOAST_DURATIONS.error).toBe(3500);
  });
});
