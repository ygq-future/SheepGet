/**
 * Content-Type 里「这个响应是页面/脚本本身，不是要下载的文件内容」的那一组。
 *
 * 它存在的理由是后缀并不总能说明内容：`.ts` 既是 MPEG-TS 分片也是 TypeScript 源码，
 * 签名过期时 CDN 还会用 `.mp4` 的 URL 返回一个 HTML 错误页。这两条链路（资源嗅探与
 * 下载接管）都要用同一条判断，所以放在这里，两边各取一份同样的语义。
 */
const DOCUMENT_OR_SCRIPT_MIMES = new Set([
  'application/javascript',
  'text/javascript',
  'application/x-javascript',
  'application/ecmascript',
  'text/ecmascript',
  'application/typescript',
  'text/typescript',
  'text/html',
  'application/xhtml+xml',
  'application/json',
  'text/css',
]);

/** 取 Content-Type 的媒体类型部分：去掉 `; charset=...` 之类的参数并统一小写。 */
export function normalizeMime(contentTypeHeader?: string): string {
  return (contentTypeHeader || '').split(';')[0]?.toLowerCase().trim() || '';
}

export function isDocumentOrScriptMime(contentTypeHeader?: string): boolean {
  return DOCUMENT_OR_SCRIPT_MIMES.has(normalizeMime(contentTypeHeader));
}
