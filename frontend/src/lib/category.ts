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
 * Resolves the matching CategoryConfig for a given filename based on:
 * 1. Custom categories (first match wins, top-to-bottom).
 * 2. Builtin non-file categories.
 * 3. Explicit extension match in builtin "文件" category.
 * 4. Unmatched fallback into builtin "文件" category (or default directory).
 */
export function resolveCategory(
  filename: string,
  settings: configModels.Settings | null,
): { category: configModels.CategoryConfig | null; directory: string } {
  const defaultDir = settings?.download?.defaultDirectory || '';
  const ext = extractExtension(filename);

  const customCategories = settings?.download?.customCategories || [];
  const builtinCategories = settings?.download?.builtinCategories || [];

  if (ext) {
    // 1. Custom categories (top to bottom)
    for (const cat of customCategories) {
      const normalized = normalizeExtensions(cat.extensions || []);
      if (normalized.includes(ext)) {
        return {
          category: cat,
          directory: cat.directory || defaultDir,
        };
      }
    }

    // 2. Builtin non-file categories
    let fileCategory: configModels.CategoryConfig | null = null;
    for (const cat of builtinCategories) {
      if (cat.id === 'builtin-file' || cat.name === '文件') {
        fileCategory = cat;
        continue;
      }
      const normalized = normalizeExtensions(cat.extensions || []);
      if (normalized.includes(ext)) {
        return {
          category: cat,
          directory: cat.directory || defaultDir,
        };
      }
    }

    // 3. Explicit check in file category
    if (fileCategory) {
      const normalized = normalizeExtensions(fileCategory.extensions || []);
      if (normalized.includes(ext)) {
        return {
          category: fileCategory,
          directory: fileCategory.directory || defaultDir,
        };
      }
    }
  }

  // 4. Fallback: unmatched files enter "文件" category
  for (const cat of builtinCategories) {
    if (cat.id === 'builtin-file' || cat.name === '文件') {
      return {
        category: cat,
        directory: cat.directory || defaultDir,
      };
    }
  }

  return {
    category: null,
    directory: defaultDir,
  };
}
