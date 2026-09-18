import * as taskModels from '../../bindings/sheep-get/internal/task/models';

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
