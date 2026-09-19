import type { TakeoverConfigSync } from './types';
import { isDocumentOrScriptMime } from './mimetypes';
import { isKeyPressed } from './shortcuts';

export function extractExtension(filenameOrUrl: string): string {
  if (!filenameOrUrl) return '';
  let clean = filenameOrUrl;
  try {
    if (clean.includes('://')) {
      const parsed = new URL(clean);
      clean = parsed.pathname;
    }
  } catch {
    // Ignore URL parse error, use raw string
  }
  // Strip query and hash if present in string
  clean = (clean.split('?')[0] ?? '').split('#')[0] ?? '';
  const lastSlash = Math.max(clean.lastIndexOf('/'), clean.lastIndexOf('\\'));
  const basename = lastSlash >= 0 ? clean.slice(lastSlash + 1) : clean;
  const lastDot = basename.lastIndexOf('.');
  if (lastDot <= 0 || lastDot === basename.length - 1) {
    return '';
  }
  return basename
    .slice(lastDot + 1)
    .toLowerCase()
    .trim();
}

export function isExtensionMatched(filenameOrUrl: string, allowedExtensions: string[]): boolean {
  const ext = extractExtension(filenameOrUrl);
  if (!ext) return false;
  for (const allowed of allowedExtensions) {
    const cleanAllowed = allowed.replace(/^\./, '').toLowerCase().trim();
    if (cleanAllowed === ext) {
      return true;
    }
  }
  return false;
}

export function extractHostname(urlStr: string): string {
  if (!urlStr) return '';
  try {
    const parsed = new URL(urlStr.includes('://') ? urlStr : `http://${urlStr}`);
    return parsed.hostname.toLowerCase().trim();
  } catch {
    return urlStr.toLowerCase().trim();
  }
}

export function isSiteExcluded(pageUrl: string, excludedSites: string[]): boolean {
  const host = extractHostname(pageUrl);
  if (!host) return false;

  for (const pattern of excludedSites) {
    const norm = pattern.toLowerCase().trim();
    if (!norm) continue;

    // Exact match
    if (host === norm) return true;

    // Wildcard prefix like "*.github.com"
    if (norm.startsWith('*.')) {
      const root = norm.slice(2);
      if (host === root || host.endsWith(`.${root}`)) {
        return true;
      }
    }

    // Normal domain like "github.com" matches subdomains "api.github.com"
    if (!norm.includes('*')) {
      if (host.endsWith(`.${norm}`)) {
        return true;
      }
    }

    // General wildcard matching (e.g. "*cdn*", "dl-*.site.com")
    if (norm.includes('*')) {
      const regexStr = '^' + norm.replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*') + '$';
      try {
        const reg = new RegExp(regexStr);
        if (reg.test(host)) return true;
      } catch {
        // Invalid regex, skip
      }
    }
  }
  return false;
}

export interface TakeoverDecision {
  takeover: boolean;
  reason:
    | 'pause_shortcut_active'
    | 'force_shortcut_active'
    | 'site_excluded'
    | 'extension_matched'
    | 'document_mime'
    | 'extension_not_matched';
}

export function decideTakeover(
  url: string,
  filenameSuggestion: string | undefined,
  pageUrl: string | undefined,
  config: TakeoverConfigSync,
  keyMask: number,
  mimeType?: string,
): TakeoverDecision {
  // 1. Pause shortcut bypasses everything
  if (config.pauseShortcut && isKeyPressed(keyMask, config.pauseShortcut)) {
    return { takeover: false, reason: 'pause_shortcut_active' };
  }

  // 2. Force shortcut forces takeover regardless of extensions or site exclusions
  if (config.forceShortcut && isKeyPressed(keyMask, config.forceShortcut)) {
    return { takeover: true, reason: 'force_shortcut_active' };
  }

  // 3. Check site exclusion against pageUrl (host page)
  if (pageUrl && isSiteExcluded(pageUrl, config.excludedSites)) {
    return { takeover: false, reason: 'site_excluded' };
  }

  // 4. Check extension match
  const target = filenameSuggestion || url;
  if (!isExtensionMatched(target, config.extensions)) {
    return { takeover: false, reason: 'extension_not_matched' };
  }

  // 5. 后缀命中，但响应头说这只是页面/脚本本身（`main.ts` 是 TypeScript 源码、签名过期后
  //    CDN 用 `.mp4` 的 URL 返回 HTML 错误页）——这不是用户要下载的文件。
  //    它只否决「按后缀猜出来的」接管：强制快捷键（第 2 步）是用户的明确意图，站点排除在
  //    第 3 步，两者都保持更高优先级；拿不到 MIME 时不猜也不否决，回落到按后缀判定。
  if (isDocumentOrScriptMime(mimeType)) {
    return { takeover: false, reason: 'document_mime' };
  }

  return { takeover: true, reason: 'extension_matched' };
}
