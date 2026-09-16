import * as taskModels from '../../bindings/sheep-get/internal/task/models';

export interface MergedSegment {
  start: number;
  end: number;
  startPercent: number;
  widthPercent: number;
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
