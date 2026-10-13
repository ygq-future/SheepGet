import { describe, it, expect } from 'vitest';
import {
  formatBytes,
  formatSpeed,
  formatDuration,
  formatDateTime,
  formatTimeHMS,
} from './lib/format';

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

  it('formats duration correctly', () => {
    expect(formatDuration(0)).toBe('00:00');
    expect(formatDuration(-10)).toBe('00:00');
    expect(formatDuration(45)).toBe('00:45');
    expect(formatDuration(125)).toBe('02:05');
    expect(formatDuration(3600)).toBe('01:00:00');
    expect(formatDuration(3665)).toBe('01:01:05');
  });

  it('formats time to dynamic HMS correctly', () => {
    expect(formatTimeHMS(0)).toBe('0s');
    expect(formatTimeHMS(-10)).toBe('0s');
    expect(formatTimeHMS(45)).toBe('45s');
    expect(formatTimeHMS(59)).toBe('59s');
    expect(formatTimeHMS(60)).toBe('1m00s');
    expect(formatTimeHMS(65)).toBe('1m05s');
    expect(formatTimeHMS(320)).toBe('5m20s');
    expect(formatTimeHMS(3599)).toBe('59m59s');
    expect(formatTimeHMS(3600)).toBe('1h00m00s');
    expect(formatTimeHMS(3665)).toBe('1h01m05s');
    expect(formatTimeHMS(3920)).toBe('1h05m20s');
    expect(formatTimeHMS(7384)).toBe('2h03m04s');
  });

  it('formats date time correctly', () => {
    expect(formatDateTime(undefined)).toBe('-');
    expect(formatDateTime('')).toBe('-');
    expect(formatDateTime('invalid-date')).toBe('-');
    const sample = new Date('2026-09-16T14:30:00Z');
    const formatted = formatDateTime(sample.toISOString());
    expect(formatted).toMatch(/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}$/);
  });
});
