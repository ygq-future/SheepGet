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
