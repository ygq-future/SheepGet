import { describe, it, expect } from 'vitest';
import { CLEANUP_DAYS_OPTIONS, DEFAULT_CLEANUP_CONFIG, extractCleanupSummary } from './lib/cleanup';
import { CleanupScanResult } from '../bindings/sheep-get/internal/engine/models';

describe('cleanup helpers and summary calculation', () => {
  it('defines valid non-empty days options without unlimited option', () => {
    expect(CLEANUP_DAYS_OPTIONS.length).toBeGreaterThan(0);
    // Ensure no "unlimited" or <= 0 options exist
    for (const opt of CLEANUP_DAYS_OPTIONS) {
      expect(opt.value).toBeGreaterThan(0);
      expect(typeof opt.label).toBe('string');
    }
  });

  it('defaults to one-click cleaning both records and files', () => {
    expect(DEFAULT_CLEANUP_CONFIG.olderTasksEnabled).toBe(true);
    expect(DEFAULT_CLEANUP_CONFIG.deleteOlderFiles).toBe(true);
    expect(DEFAULT_CLEANUP_CONFIG.duplicatesEnabled).toBe(true);
    expect(DEFAULT_CLEANUP_CONFIG.missingTasksEnabled).toBe(true);
    expect(DEFAULT_CLEANUP_CONFIG.oldLogsEnabled).toBe(true);
  });

  it('returns zeroes when scanResult is null', () => {
    const summary = extractCleanupSummary(null);
    expect(summary).toEqual({ totalTasks: 0, totalFiles: 0, totalBytes: 0 });
  });

  it('extracts authoritative deduplicated totals directly from scanResult', () => {
    const scanResult = new CleanupScanResult({
      totalCleanableTasks: 5,
      totalCleanableFiles: 3,
      totalFreedBytes: 1024 * 1024 * 50,
    });

    const summary = extractCleanupSummary(scanResult);
    expect(summary.totalTasks).toBe(5);
    expect(summary.totalFiles).toBe(3);
    expect(summary.totalBytes).toBe(1024 * 1024 * 50);
  });
});
