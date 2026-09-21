import * as configModels from '../../bindings/sheep-get/internal/config/models';

/**
 * Normalizes extension strings or arrays into cleaned lowercase extensions without dots.
 */
export function normalizeExtensions(input: string[] | string): string[] {
  const exts = Array.isArray(input)
    ? input
    : input
        .split(/[,，\s]+/)
        .map((s) => s.trim())
        .filter(Boolean);

  const seen = new Set<string>();
  const result: string[] = [];

  for (const ext of exts) {
    const clean = ext.trim().replace(/^\.+/, '').toLowerCase();
    if (clean && !seen.has(clean)) {
      seen.add(clean);
      result.push(clean);
    }
  }

  return result;
}

/**
 * Extracts file extension from a filename or URL path.
 */
export function extractExtension(filename: string): string {
  const cleanName = filename.split('?')[0].split('#')[0];
  const lastDot = cleanName.lastIndexOf('.');
  if (lastDot === -1 || lastDot === cleanName.length - 1) {
    return '';
  }
  return cleanName
    .slice(lastDot + 1)
    .toLowerCase()
    .trim();
}

/**
 * Matches a task's filename against current settings category rules.
 * Follows the exact precedence of backend config.ResolveCategory:
 * 1. Custom categories (first match in order)
 * 2. Non-file built-in categories
 * 3. File built-in category
 * 4. Fallback to builtin-file
 */
export function matchTaskCategory(
  filename: string,
  settings: configModels.Settings | null | undefined,
): string {
  const ext = extractExtension(filename);
  const download = settings?.download;

  if (ext && download) {
    // 1. Custom categories (first match wins, top-to-bottom)
    for (const cat of download.customCategories || []) {
      for (const catExt of cat.extensions || []) {
        if (catExt.replace(/^\.+/, '').toLowerCase() === ext) {
          return cat.id;
        }
      }
    }

    // 2. Built-in non-file categories
    let fileCat: configModels.CategoryConfig | undefined;
    for (const cat of download.builtinCategories || []) {
      if (cat.id === 'builtin-file' || cat.name === '文件') {
        fileCat = cat;
        continue;
      }
      for (const catExt of cat.extensions || []) {
        if (catExt.replace(/^\.+/, '').toLowerCase() === ext) {
          return cat.id;
        }
      }
    }

    // 3. Explicit file category extensions
    if (fileCat) {
      for (const catExt of fileCat.extensions || []) {
        if (catExt.replace(/^\.+/, '').toLowerCase() === ext) {
          return fileCat.id;
        }
      }
    }
  }

  // 4. Fallback: unmatched files enter "builtin-file"
  const builtinFile = (download?.builtinCategories || []).find(
    (c) => c.id === 'builtin-file' || c.name === '文件',
  );
  return builtinFile?.id || 'builtin-file';
}
