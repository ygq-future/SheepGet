import * as taskModels from '../../bindings/sheep-get/internal/task/models';

export interface MergedSegment {
  start: number;
  end: number;
  startPercent: number;
  widthPercent: number;
}

export interface ChunkProgressItem {
  id: string;
  index: number;
  start: number;
  end: number;
  chunkSize: number;
  startPercent: number;
  widthPercent: number;
  downloadedPercent: number;
  downloaded: number;
  completed: boolean;
  assisted: boolean;
}

export interface ChunkDivider {
  id: string;
  positionPercent: number;
  assisted: boolean;
  targetChunkIndex: number;
}

/**
 * Calculates progress items for each individual chunk with stable keys and physical positions,
 * ensuring zero layout jumps or misattributed animations when chunks progress or merge.
 */
export function calculateChunkProgress(
  chunks: taskModels.Chunk[] | undefined,
  totalBytes: number,
): ChunkProgressItem[] {
  if (!chunks || chunks.length === 0 || totalBytes <= 0) {
    return [];
  }

  return chunks.map((chunk) => {
    const chunkSize = Math.max(1, chunk.end - chunk.start + 1);
    const startPercent = Math.max(0, Math.min(100, (chunk.start / totalBytes) * 100));
    const widthPercent = Math.max(0, Math.min(100 - startPercent, (chunkSize / totalBytes) * 100));
    const validDownloaded = chunk.completed
      ? chunkSize
      : Math.min(chunkSize, Math.max(0, chunk.downloaded));
    const downloadedPercent = Math.max(
      0,
      Math.min(widthPercent, (validDownloaded / totalBytes) * 100),
    );

    return {
      id: `chunk-${chunk.index}`,
      index: chunk.index,
      start: chunk.start,
      end: chunk.end,
      chunkSize,
      startPercent,
      widthPercent,
      downloadedPercent,
      downloaded: validDownloaded,
      completed: chunk.completed,
      assisted: !!chunk.assisted,
    };
  });
}

/**
 * Calculates physical chunk boundary dividers in spatial ascending order,
 * clearly marking split points including dynamic assisted slow chunk splits in-place.
 */
export function calculateChunkDividers(
  chunks: taskModels.Chunk[] | undefined,
  totalBytes: number,
): ChunkDivider[] {
  if (!chunks || chunks.length <= 1 || totalBytes <= 0) {
    return [];
  }

  // Sort chunks by start position ascending so dividers match spatial sequence regardless of array append order
  const sorted = [...chunks].sort((a, b) => a.start - b.start);
  const dividers: ChunkDivider[] = [];

  for (let i = 1; i < sorted.length; i++) {
    const c = sorted[i];
    const positionPercent = Math.max(0, Math.min(100, (c.start / totalBytes) * 100));
    dividers.push({
      id: `divider-${c.index}-${c.start}`,
      positionPercent,
      assisted: !!c.assisted,
      targetChunkIndex: c.index,
    });
  }

  return dividers;
}

/**
 * Merges continuous or overlapping downloaded ranges across multiple chunks,
 * producing a unified progress track where adjacent completed/downloaded chunks seamlessly merge.
 */
export function mergeProgressSegments(
  chunks: taskModels.Chunk[] | undefined,
  totalBytes: number,
  downloaded: number,
): MergedSegment[] {
  if (totalBytes <= 0) {
    return [];
  }

  // If no chunks are provided, fallback to single range from 0 to downloaded
  if (!chunks || chunks.length === 0) {
    if (downloaded <= 0) return [];
    const validDownloaded = Math.min(downloaded, totalBytes);
    const width = Math.min(100, (validDownloaded / totalBytes) * 100);
    return [
      {
        start: 0,
        end: validDownloaded - 1,
        startPercent: 0,
        widthPercent: width,
      },
    ];
  }

  // Extract downloaded ranges from each chunk
  const rawRanges: Array<[number, number]> = [];

  for (const chunk of chunks) {
    if (chunk.downloaded <= 0 && !chunk.completed) {
      continue;
    }
    const start = Math.max(0, chunk.start);
    const maxEnd = Math.max(start, chunk.end);
    let end: number;

    if (chunk.completed) {
      end = maxEnd;
    } else {
      end = Math.min(maxEnd, start + chunk.downloaded - 1);
    }

    if (end >= start) {
      rawRanges.push([start, end]);
    }
  }

  if (rawRanges.length === 0) {
    return [];
  }

  // Sort ranges by start position ascending
  rawRanges.sort((a, b) => a[0] - b[0]);

  // Merge overlapping and contiguous intervals
  const merged: Array<[number, number]> = [];
  let cur = rawRanges[0];

  for (let i = 1; i < rawRanges.length; i++) {
    const next = rawRanges[i];
    // If intervals touch (e.g. 0..99 and 100..199, where 100 <= 99 + 1) or overlap
    if (next[0] <= cur[1] + 1) {
      cur = [cur[0], Math.max(cur[1], next[1])];
    } else {
      merged.push(cur);
      cur = next;
    }
  }
  merged.push(cur);

  // Convert to percentages
  return merged.map(([start, end]) => {
    const clampedEnd = Math.min(end, totalBytes - 1);
    const startPercent = Math.max(0, Math.min(100, (start / totalBytes) * 100));
    const widthPercent = Math.max(
      0,
      Math.min(100 - startPercent, ((clampedEnd - start + 1) / totalBytes) * 100),
    );
    return {
      start,
      end: clampedEnd,
      startPercent,
      widthPercent,
    };
  });
}

/**
 * 处理失败与传输失败是两条不同的恢复路径：只有处理阶段失败的任务才提供「重试处理」，
 * 且重试复用已下载分片而不是重新传输。判定来自任务契约的 failurePhase，
 * 不从错误文案推断失败类型。
 */
export function isProcessingFailure(t: taskModels.Task): boolean {
  return (
    t.status === taskModels.Status.StatusError &&
    t.failurePhase === taskModels.FailurePhase.FailurePhaseProcessing
  );
}
