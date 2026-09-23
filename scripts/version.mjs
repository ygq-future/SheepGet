// Product version consistency. build/config.yml `info.version` is the single source of
// truth; every other copy is declared once in MIRRORS below, so `--check` fails the
// quality gate when a copy drifts and `--set X.Y.Z` rewrites all of them in one pass.
import { readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SSOT_FILE = 'build/config.yml';

// Numeric X.Y.Z is the only form Windows PE version resources, MSI ProductVersion and the
// Chrome manifest version accept; a fourth component or a pre-release suffix is rejected.
const VERSION_PATTERN = /^\d+\.\d+\.\d+$/;

/**
 * Mirrors of the SSOT version, each with the exact wording it carries. The pattern
 * captures the bare version in `?<version>`, so `--set` rewrites precisely that span and
 * leaves surrounding quotes, `v` prefixes and artifact names untouched. Every entry must
 * keep at least one match: a renamed key or rewritten section fails the check instead of
 * silently dropping a copy.
 */
const MIRRORS = [
  {
    file: 'package.json',
    label: 'version',
    pattern: /"version":\s*"(?<version>\d+\.\d+\.\d+)"/dg,
  },
  {
    file: 'build/windows/info.json',
    label: 'file_version',
    pattern: /"file_version":\s*"(?<version>\d+\.\d+\.\d+)"/dg,
  },
  {
    file: 'build/windows/info.json',
    label: 'ProductVersion',
    pattern: /"ProductVersion":\s*"(?<version>\d+\.\d+\.\d+)"/dg,
  },
  {
    file: 'extension/package.json',
    label: 'version',
    pattern: /"version":\s*"(?<version>\d+\.\d+\.\d+)"/dg,
  },
  {
    file: 'extension/wxt.config.ts',
    label: 'manifest.version',
    pattern: /version:\s*'(?<version>\d+\.\d+\.\d+)'/dg,
  },
  {
    file: 'extension/entrypoints/popup/App.tsx',
    label: 'displayed version',
    pattern: /v(?<version>\d+\.\d+\.\d+)<\/span>/dg,
  },
  {
    file: '.github/workflows/release.yml',
    label: 'release tag',
    pattern: /v(?<version>\d+\.\d+\.\d+)(?=')/dg,
  },
  {
    file: 'README.md',
    label: 'artifact name',
    pattern: /SheepGet_(?<version>\d+\.\d+\.\d+)_/dg,
  },
  {
    file: 'internal/version/version.go',
    label: 'version.Version',
    pattern: /Version = "(?<version>\d+\.\d+\.\d+)"/dg,
  },
];

/** Parses the flat `key: 'value'` lines of the `info:` block and records value spans. */
function parseInfoBlock(text) {
  const header = /^info:[ \t]*\r?\n/m.exec(text);
  if (!header) throw new Error(`${SSOT_FILE} must declare an info block`);
  const bodyStart = header.index + header[0].length;
  const rest = text.slice(bodyStart);
  const nextBlock = rest.search(/^\S/m);
  const body = nextBlock === -1 ? rest : rest.slice(0, nextBlock);

  const values = {};
  const spans = {};
  for (const match of body.matchAll(/(?<key>[A-Za-z]\w*):[ \t]*'(?<value>[^'\r\n]*)'/dg)) {
    values[match.groups.key] = match.groups.value;
    const [start, end] = match.indices.groups.value;
    spans[match.groups.key] = [bodyStart + start, bodyStart + end];
  }
  return { values, spans };
}

/** Values declared by the `info:` block of build/config.yml; `version` is the product version. */
export function readInfo(root) {
  return parseInfoBlock(readFileSync(join(root, SSOT_FILE), 'utf8')).values;
}

/** Every file carrying the product version: the SSOT first, then each mirror. */
export function versionFiles() {
  return [SSOT_FILE, ...new Set(MIRRORS.map((mirror) => mirror.file))];
}

function lineNumber(text, index) {
  return text.slice(0, index).split('\n').length;
}

function replaceSpans(text, spans, version) {
  let out = '';
  let cursor = 0;
  for (const [start, end] of spans) {
    out += text.slice(cursor, start) + version;
    cursor = end;
  }
  return out + text.slice(cursor);
}

/**
 * Throws listing every source that disagrees with the SSOT version; returns that version.
 */
export function checkVersion(root) {
  const version = readInfo(root).version;
  if (typeof version !== 'string' || !VERSION_PATTERN.test(version)) {
    throw new Error(`${SSOT_FILE} info.version must be X.Y.Z, found ${JSON.stringify(version)}`);
  }

  const drift = [];
  for (const { file, label, pattern } of MIRRORS) {
    const text = readFileSync(join(root, file), 'utf8');
    const matches = [...text.matchAll(pattern)];
    if (!matches.length) {
      drift.push(`${file}: no ${label} declaration`);
      continue;
    }
    for (const match of matches) {
      const found = match.groups.version;
      if (found === version) continue;
      const line = lineNumber(text, match.indices.groups.version[0]);
      drift.push(`${file}:${line}: ${label} is ${found}, expected ${version}`);
    }
  }
  if (drift.length) {
    throw new Error(
      `${drift.length} version source(s) disagree with ${SSOT_FILE} (${version}):\n  ${drift.join('\n  ')}`,
    );
  }
  return version;
}

/**
 * Rewrites the SSOT and every mirror to `version`; returns the files actually changed.
 */
export function setVersion(root, version) {
  if (!VERSION_PATTERN.test(version)) {
    throw new Error(`Version must be X.Y.Z, found ${JSON.stringify(version)}`);
  }

  const changed = [];
  const write = (file, path, text, spans) => {
    if (!spans.length) throw new Error(`${file}: no version declaration to update`);
    const next = replaceSpans(
      text,
      spans.sort((left, right) => left[0] - right[0]),
      version,
    );
    if (next === text) return;
    writeFileSync(path, next);
    changed.push(file);
  };

  const configPath = join(root, SSOT_FILE);
  const configText = readFileSync(configPath, 'utf8');
  const { spans } = parseInfoBlock(configText);
  if (!spans.version) throw new Error(`${SSOT_FILE} info block must define version`);
  write(SSOT_FILE, configPath, configText, [spans.version]);

  const patterns = new Map();
  for (const { file, pattern } of MIRRORS) {
    patterns.set(file, [...(patterns.get(file) ?? []), pattern]);
  }
  for (const [file, filePatterns] of patterns) {
    const path = join(root, file);
    const text = readFileSync(path, 'utf8');
    write(
      file,
      path,
      text,
      filePatterns.flatMap((pattern) =>
        [...text.matchAll(pattern)].map((match) => match.indices.groups.version),
      ),
    );
  }
  return changed;
}

function main(argv) {
  const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
  const [command, value] = argv;
  if (command === '--check') {
    const version = checkVersion(root);
    console.log(`[Version] ${version} consistent across ${versionFiles().length} files`);
    return;
  }
  if (command === '--set' && value) {
    const changed = setVersion(root, value);
    checkVersion(root);
    const detail = changed.length ? changed.join(', ') : 'already in sync';
    console.log(`[Version] ${value} applied to ${changed.length} file(s): ${detail}`);
    return;
  }
  throw new Error('Usage: node scripts/version.mjs --check | --set X.Y.Z');
}

if (resolve(process.argv[1] ?? '') === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
