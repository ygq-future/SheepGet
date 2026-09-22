import { CleanupScanResult } from '../../bindings/sheep-get/internal/engine/models';

export const CLEANUP_DAYS_OPTIONS = [
  { value: 30, label: '30 天以前' },
  { value: 90, label: '3 个月以前 (90天)' },
  { value: 180, label: '半年以前 (180天)' },
] as const;

export interface CleanupConfigState {
  olderTasksEnabled: boolean;
  olderThanDays: number;
  deleteOlderFiles: boolean;
  duplicatesEnabled: boolean;
  missingTasksEnabled: boolean;
}

export const DEFAULT_CLEANUP_CONFIG: CleanupConfigState = {
  olderTasksEnabled: true,
  olderThanDays: 30,
  deleteOlderFiles: true,
  duplicatesEnabled: true,
  missingTasksEnabled: true,
};

export interface CleanupSummary {
  totalTasks: number;
  totalFiles: number;
  totalBytes: number;
}

/**
 * extractCleanupSummary delegates authoritative metrics directly from backend scan results,
 * respecting the Single Source of Truth architecture (ADR-0002 / AGENTS.md).
 */
export function extractCleanupSummary(scanResult: CleanupScanResult | null): CleanupSummary {
  if (!scanResult) {
    return { totalTasks: 0, totalFiles: 0, totalBytes: 0 };
  }
  return {
    totalTasks: scanResult.totalCleanableTasks ?? 0,
    totalFiles: scanResult.totalCleanableFiles ?? 0,
    totalBytes: scanResult.totalFreedBytes ?? 0,
  };
}
