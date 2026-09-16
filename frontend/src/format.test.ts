import { describe, it, expect } from 'vitest';
import { formatBytes, formatSpeed, formatDuration, formatDateTime } from './lib/format';

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

  it('formats date time correctly', () => {
    expect(formatDateTime(undefined)).toBe('-');
    expect(formatDateTime('')).toBe('-');
    expect(formatDateTime('invalid-date')).toBe('-');
    const sample = new Date('2026-09-16T14:30:00Z');
    const formatted = formatDateTime(sample.toISOString());
    expect(formatted).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
  });
});
