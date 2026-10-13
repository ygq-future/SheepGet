export function formatBytes(bytes: number, decimals = 2): string {
  if (bytes < 0) return '未知大小';
  if (bytes === 0) return '0 B';
  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
}

export function formatSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
  return `${formatBytes(bytesPerSec)}/s`;
}

export function formatDuration(seconds: number): string {
  if (!seconds || isNaN(seconds) || seconds <= 0) return '00:00';
  const totalSecs = Math.floor(seconds);
  const hrs = Math.floor(totalSecs / 3600);
  const mins = Math.floor((totalSecs % 3600) / 60);
  const secs = totalSecs % 60;

  const pad = (n: number) => n.toString().padStart(2, '0');
  if (hrs > 0) {
    return `${pad(hrs)}:${pad(mins)}:${pad(secs)}`;
  }
  return `${pad(mins)}:${pad(secs)}`;
}

/**
 * formatTimeHMS 格式化下载耗时或预估剩余时间为动态省略的 xxhxxmxxs。
 * 规则：
 * - 不超出分钟：只显示秒（如 45s、0s）；
 * - 不超出小时：只显示分秒，分钟无前导零，秒补双位（如 5m20s、0m05s）；
 * - 超出小时：显示时分秒，小时无前导零，分秒补双位（如 1h05m20s）。
 */
export function formatTimeHMS(seconds: number): string {
  if (!seconds || isNaN(seconds) || seconds <= 0) return '0s';
  const totalSecs = Math.floor(seconds);
  const hrs = Math.floor(totalSecs / 3600);
  const mins = Math.floor((totalSecs % 3600) / 60);
  const secs = totalSecs % 60;

  const pad = (n: number) => n.toString().padStart(2, '0');
  if (hrs > 0) {
    return `${hrs}h${pad(mins)}m${pad(secs)}s`;
  }
  if (mins > 0) {
    return `${mins}m${pad(secs)}s`;
  }
  return `${secs}s`;
}

export function formatDateTime(dateStr?: string): string {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  if (isNaN(d.getTime()) || d.getTime() <= 0) return '-';
  const pad = (n: number) => n.toString().padStart(2, '0');
  const year = d.getFullYear();
  const month = pad(d.getMonth() + 1);
  const day = pad(d.getDate());
  const hours = pad(d.getHours());
  const mins = pad(d.getMinutes());
  return `${year}/${month}/${day} ${hours}:${mins}`;
}
