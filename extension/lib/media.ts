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
  /** 后台预探测到的展示信息（时长/大小/清晰度数），探测完成后回填到池内资源上。 */
  probeDuration?: number;
  probeTotalBytes?: number;
  probeVariants?: number;
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

/**
 * 判断一次请求是不是 HLS 分片。分片是清单的内部实现，真正要下载的是 m3u8 清单，
 * 所以资源池把它过滤掉，避免面板被几百个 seg*.ts 刷屏、悬浮条兜底关联误命中分片。
 * 判据是「MPEG-TS 内容」：`audio/mp2t` / `video/mp2t`，或 `.ts` 后缀且 MIME 是 ts 类型。
 */
export function isHlsSegment(url: string, contentTypeHeader?: string): boolean {
  const mime = normalizeMime(contentTypeHeader);
  if (mime === 'video/mp2t' || mime === 'audio/mp2t') return true;
  const ext = extractExtension(url);
  if (ext === 'ts' && (mime.startsWith('video/') || mime.startsWith('audio/'))) return true;
  return false;
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

/**
 * 从页面 DOM 推断视频/资源的真实名称，与 IDM 的做法对齐：
 * og:title（站点专门为分享准备的标题）→ 页面 <title>。两者都做同样的清洗：
 * 去掉 `(数字)` 这类番号前缀、折叠空白、去掉常见的播放/片头占位符。
 * 拿不到任何标题时返回空串，由调用方回退到 URL 末段。
 */
export function inferPageTitle(doc: Pick<Document, 'querySelector' | 'title'>): string {
  const ogTitle = doc.querySelector('meta[property="og:title"]')?.getAttribute('content') || '';
  const raw = (ogTitle || doc.title || '').trim();
  if (!raw) return '';

  const cleaned = raw
    // 去掉 `(12)` / `（12）` 这类播放列表番号前缀（IDM 的正则 `^\(\d+\)`）
    .replace(/^[(（]\d+[)）]\s*/, '')
    // 折叠换行/制表符/重复空格，避免多行标题带进文件名
    .replace(/[ \t\r\n]+/g, ' ')
    .trim();
  return cleaned;
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

/** 把秒数格式化成 mm:ss（或 h:mm:ss）；探不出时长时返回空串，调用方按没有处理。 */
export function formatDuration(seconds?: number): string {
  if (seconds === undefined || seconds === null || seconds <= 0) return '';
  const total = Math.round(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  return `${m}:${String(s).padStart(2, '0')}`;
}

/**
 * 把完整 MIME 压成面板一行能放下的短标签。`application/vnd.apple.mpegurl` 这类
 * 全称没有信息量还挤占版面：清单按 m3u8 显示，其余取子类型（video/mp4 → mp4）。
 */
export function mimeShortLabel(mime?: string): string {
  if (!mime) return '';
  const subtype = mime.split('/')[1] || mime;
  if (subtype.includes('mpegurl')) return 'm3u8';
  return subtype;
}
