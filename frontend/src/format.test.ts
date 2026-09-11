import { describe, it, expect } from 'vitest';
import { formatBytes, formatSpeed } from './lib/format';

describe('format utilities', () => {
  it('formats byte sizes correctly', () => {
    expect(formatBytes(-1)).toBe('未知大小');
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1024 * 1024)).toBe('1 MB');
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1 GB');
  });

  it('formats transfer speed correctly', () => {
    expect(formatSpeed(0)).toBe('0 B/s');
    expect(formatSpeed(-5)).toBe('0 B/s');
    expect(formatSpeed(1024)).toBe('1 KB/s');
    expect(formatSpeed(5 * 1024 * 1024)).toBe('5 MB/s');
  });
});
