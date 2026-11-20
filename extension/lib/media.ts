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

// 网页/服务端脚本/非媒体资源后缀：DOM 兜底扫描时遇到这类链接绝不能盲目当成 MP4。
// 若这类地址确实返回 video/* 或 m3u8 响应，网络层 onHeadersReceived 会凭 Content-Type 捕获。
const NON_MEDIA_EXTENSIONS: Record<string, true> = {
  php: true,
  phtml: true,
  html: true,
  htm: true,
  xhtml: true,
  shtml: true,
  asp: true,
  aspx: true,
  jsp: true,
  jspx: true,
  cgi: true,
  pl: true,
  do: true,
  action: true,
  json: true,
  xml: true,
  js: true,
  jsx: true,
  mjs: true,
  cjs: true,
  ts: true,
  tsx: true,
  css: true,
  svg: true,
  png: true,
  jpg: true,
  jpeg: true,
  gif: true,
  webp: true,
  ico: true,
  txt: true,
  vtt: true,
  srt: true,
  ass: true,
};

const MIME_TO_EXT: Record<string, string> = {
  'video/mp4': '.mp4',
  'video/webm': '.webm',
  'video/x-matroska': '.mkv',
  'video/quicktime': '.mov',
  'video/x-flv': '.flv',
  'video/x-msvideo': '.avi',
  'video/x-ms-wmv': '.wmv',
  'video/mp2t': '.ts',
  'audio/mpeg': '.mp3',
  'audio/mp3': '.mp3',
  'audio/mp4': '.m4a',
  'audio/x-m4a': '.m4a',
  'audio/aac': '.aac',
  'audio/ogg': '.ogg',
  'audio/opus': '.opus',
  'audio/wav': '.wav',
  'audio/x-wav': '.wav',
  'audio/flac': '.flac',
  'audio/webm': '.weba',
};

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

export function isKnownMediaExtension(ext: string): boolean {
  const clean = ext.toLowerCase().replace(/^\./, '').trim();
  return MEDIA_EXTENSIONS.has(clean) || HLS_EXTENSIONS.has(clean) || clean === 'ts';
}

function stripUrlHash(rawUrl: string): string {
  const idx = rawUrl.indexOf('#');
  return idx >= 0 ? rawUrl.slice(0, idx) : rawUrl;
}

/**
 * 判断从 DOM (<video>/<source>) 提取出的链接是否值得作为兜底媒体资源入池。
 * 拦截 `.php` / `.html` 等网页或脚本地址，以及回指当前页面本身的空 src 解析结果。
 */
export function isLikelyDomMediaUrl(url: string, pageUrl?: string): boolean {
  if (!url || (!url.startsWith('http://') && !url.startsWith('https://'))) {
    return false;
  }
  const ext = extractExtension(url);
  if (ext && Object.hasOwn(NON_MEDIA_EXTENSIONS, ext)) {
    return false;
  }
  if (pageUrl && stripUrlHash(url) === stripUrlHash(pageUrl) && !isMediaResponse(url).isMedia) {
    return false;
  }
  return true;
}

export function mediaExtensionForMime(mime?: string, isHls = false): string {
  if (isHls) return '.m3u8';
  const normalized = normalizeMime(mime);
  if (normalized && MIME_TO_EXT[normalized]) {
    return MIME_TO_EXT[normalized];
  }
  if (normalized.startsWith('audio/')) {
    const sub = normalized.slice('audio/'.length).replace(/^x-/, '');
    if (/^[a-z0-9]{2,5}$/.test(sub)) return `.${sub}`;
    return '.m4a';
  }
  if (normalized.startsWith('video/')) {
    const sub = normalized.slice('video/'.length).replace(/^x-/, '');
    if (/^[a-z0-9]{2,5}$/.test(sub)) return `.${sub}`;
  }
  return '.mp4';
}

/**
 * 生成建议文件名。命名来源优先级与 IDM 一致：
 *   1. Content-Disposition 明确给出的文件名原样保留；
 *   2. 否则若 URL 末段是占位清单名（index.m3u8 等）、纯数字分段（123.m4s）、
 *      无后缀路径或动态脚本后缀（.php/.aspx 等）：有页面标题时用「标题 + 媒体后缀」，
 *      无标题时把非媒体后缀替换为真实的媒体后缀（绝不把 .php 当成视频后缀落盘）；
 *   3. 否则保留 URL 末段。
 */
export function suggestMediaFilename(
  fallback: string,
  detected: { isHls: boolean; mime: string },
  pageTitle?: string,
  hasExplicitFilename = false,
  sanitizeFn: (name: string) => string = (s) => s.trim(),
): string {
  if (hasExplicitFilename) return fallback;

  const urlBasename = fallback.split('?')[0]?.split('/').pop() || '';
  const ext = extractExtension(urlBasename);
  const isM4s = ext === 'm4s';
  const isPlaceholderPlaylist = /^(index|stream|playlist|master|chunklist)\.m3u8?$/i.test(
    urlBasename,
  );
  const isNumericM4s = /^\d[\d_-]*\.m4s$/i.test(urlBasename);
  const isNonMediaName = !urlBasename || !ext || !isKnownMediaExtension(ext);

  if (!isPlaceholderPlaylist && !isNumericM4s && !isNonMediaName) {
    return fallback;
  }

  if (pageTitle && pageTitle.trim()) {
    const titleSuffix =
      detected.isHls || isM4s ? '.mp4' : mediaExtensionForMime(detected.mime, false);
    return sanitizeFn(pageTitle.trim() + titleSuffix);
  }

  if (!isNonMediaName) {
    return fallback;
  }

  const targetExt = mediaExtensionForMime(detected.mime, detected.isHls);
  if (!urlBasename) {
    return `media${targetExt}`;
  }
  const dotIdx = urlBasename.lastIndexOf('.');
  const stem = dotIdx > 0 ? urlBasename.slice(0, dotIdx) : urlBasename;
  return sanitizeFn(`${stem || 'media'}${targetExt}`);
}

export function extractHttpOrigin(rawUrl?: string): string | undefined {
  if (!rawUrl) return undefined;
  try {
    const parsed = new URL(rawUrl);
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
      return parsed.origin;
    }
  } catch {
    // Ignore invalid URL
  }
  return undefined;
}

export function buildMediaRequestHeaders(
  pageUrl?: string,
  referrer?: string,
  userAgent = typeof navigator !== 'undefined' ? navigator.userAgent : '',
): Record<string, string> {
  const headers: Record<string, string> = {};
  if (userAgent) {
    headers['User-Agent'] = userAgent;
  }
  const effectiveReferer = referrer || pageUrl;
  if (
    effectiveReferer &&
    (effectiveReferer.startsWith('http://') || effectiveReferer.startsWith('https://'))
  ) {
    headers.Referer = effectiveReferer;
  }
  const origin = extractHttpOrigin(pageUrl || referrer);
  if (origin) {
    headers.Origin = origin;
  }
  return headers;
}
