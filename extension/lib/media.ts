import { extractExtension } from './rules';
import { isDocumentOrScriptMime, normalizeMime } from './mimetypes';

export interface MediaResource {
  id: string;
  url: string;
  tabId: number;
  filename: string;
  mimeType: string;
  totalBytes?: number;
  isHls: boolean;
  pageTitle?: string;
  pageUrl?: string;
  foundAt: number;
}

const HLS_EXTENSIONS = new Set(['m3u8', 'm3u']);
// 后缀是线索，不是结论。`.ts` 故意不在这里：它既可能是 MPEG-TS 分片，也可能是开发服务器
// 直接吐出来的 TypeScript 源码（`main.ts`、`router.ts`……），只看后缀会把整个 src 目录
// 列成可下载资源。这类同名的后缀只能靠 Content-Type 佐证（video/* 、audio/*）。
const MEDIA_EXTENSIONS = new Set([
  'mp4',
  'mkv',
  'flv',
  'webm',
  'avi',
  'mov',
  'wmv',
  'm4s',
  'mp3',
  'm4a',
  'aac',
  'wav',
  'flac',
  'ogg',
  'opus',
]);

const HLS_MIME_PATTERNS = [
  'application/vnd.apple.mpegurl',
  'application/x-mpegurl',
  'application/octet-stream-m3u8',
  'audio/mpegurl',
  'audio/x-mpegurl',
  'video/mpegurl',
  'video/x-mpegurl',
];

export function isMediaResponse(
  url: string,
  contentTypeHeader?: string,
): { isMedia: boolean; isHls: boolean; mime: string } {
  const mime = normalizeMime(contentTypeHeader);
  const ext = extractExtension(url);

  // 1. Check for HLS
  if (HLS_EXTENSIONS.has(ext)) {
    return { isMedia: true, isHls: true, mime: mime || 'application/vnd.apple.mpegurl' };
  }
  for (const hlsMime of HLS_MIME_PATTERNS) {
    if (mime.includes(hlsMime)) {
      return { isMedia: true, isHls: true, mime };
    }
  }

  // 2. Check for general video/audio MIME
  if (mime.startsWith('video/') || mime.startsWith('audio/')) {
    // Exclude general web types like audio/webm or video/webm if they are tiny chunks,
    // but in general webRequest headers treat them as media
    return { isMedia: true, isHls: false, mime };
  }

  // 3. Check for general video/audio URL extension
  //    后缀推定可以被 Content-Type 否认：签名过期时 CDN 会用 `.mp4` 的 URL 返回 HTML 错误页，
  //    开发服务器会用 `.ts` 的路径返回 TypeScript 源码。只否决这一条分支——按 HLS 后缀、
  //    按 HLS MIME、按 video/* 、audio/* 命中时都已经有正面证据，不受影响。
  if (MEDIA_EXTENSIONS.has(ext) && !isDocumentOrScriptMime(contentTypeHeader)) {
    return { isMedia: true, isHls: false, mime: mime || `video/${ext}` };
  }

  return { isMedia: false, isHls: false, mime };
}

export function parseContentDispositionFilename(header?: string): string {
  if (!header) return '';
  const parts = header.split(';');
  for (const part of parts) {
    const trimmed = part.trim();
    if (trimmed.toLowerCase().startsWith('filename*=')) {
      const val = trimmed.slice(10).trim();
      const utf8Match = /^(?:UTF-8'')?(.+)$/i.exec(val);
      if (utf8Match && utf8Match[1]) {
        try {
          return decodeURIComponent(utf8Match[1].replace(/['"]/g, ''));
        } catch {
          return utf8Match[1].replace(/['"]/g, '');
        }
      }
    }
    if (trimmed.toLowerCase().startsWith('filename=')) {
      const val = trimmed.slice(9).trim().replace(/['"]/g, '');
      if (val) return val;
    }
  }
  return '';
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null || bytes <= 0) {
    return '未知大小';
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let val = bytes;
  let unitIndex = 0;
  while (val >= 1024 && unitIndex < units.length - 1) {
    val /= 1024;
    unitIndex++;
  }
  return `${val.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}
