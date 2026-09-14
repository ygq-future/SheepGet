import { describe, it, expect } from 'vitest';
import { hexToRgb } from './stores/settings';

describe('settings store utilities', () => {
  it('converts 3-digit hex color to RGB correctly', () => {
    expect(hexToRgb('#fff')).toEqual({ r: 255, g: 255, b: 255 });
    expect(hexToRgb('#000')).toEqual({ r: 0, g: 0, b: 0 });
    expect(hexToRgb('#f00')).toEqual({ r: 255, g: 0, b: 0 });
  });

  it('converts 6-digit hex color to RGB correctly', () => {
    expect(hexToRgb('#10b981')).toEqual({ r: 16, g: 185, b: 129 });
    expect(hexToRgb('#3b82f6')).toEqual({ r: 59, g: 130, b: 246 });
    expect(hexToRgb('#000000')).toEqual({ r: 0, g: 0, b: 0 });
  });

  it('rejects invalid hex formats', () => {
    expect(hexToRgb('invalid')).toBeNull();
    expect(hexToRgb('#12345')).toBeNull();
    expect(hexToRgb('#1234567')).toBeNull();
    expect(hexToRgb('')).toBeNull();
  });
});
