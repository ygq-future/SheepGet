// 线上契约的 TypeScript 镜像生成器。
//
// 唯一的事实来源是 internal/protocol/protocol.go：标了 `// mirror <目标>.<分组>` 的常量会被
// 推导成 TypeScript 常量表。`--check`（门禁 stage protocol）在镜像过期时失败，`--write` 重新生成。
// 另外这里还会从 extension/wxt.config.ts 的固定公钥重新推导一次扩展 ID 并与 Go 侧比对。
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const SOURCE = 'internal/protocol/protocol.go';
const EXTENSION_CONFIG = 'extension/wxt.config.ts';
const HEADER = `// 由 scripts/protocol.mjs 从 ${SOURCE} 生成，请勿手改。
// 改动线上事实请改 Go 侧那个文件，再运行 node scripts/protocol.mjs --write。
`;

/** TypeScript 导出名：分组 → 标识符；键名是 Go 常量名去掉该分组的前缀。 */
const SECTIONS = {
  basePath: { target: 'extension', name: 'BasePath', prefix: '', scalar: true },
  paths: { target: 'extension', name: 'Paths', prefix: 'Path' },
  headers: { target: 'extension', name: 'HeaderNames', prefix: 'Header' },
  query: { target: 'extension', name: 'QueryParams', prefix: 'QueryParam' },
  ports: { name: 'Ports', prefix: 'Port' },
  wsEvents: { target: 'extension', name: 'WsEvents', prefix: 'Event' },
  events: { target: 'frontend', name: 'Event', prefix: 'Event' },
};

/** 生成文件：目标 → 相对路径。同一个目标的分组按 SECTIONS 的声明顺序排列。 */
const TARGETS = {
  extension: 'extension/lib/protocol.generated.ts',
  frontend: 'frontend/src/lib/protocol.generated.ts',
};

function fail(message) {
  throw new Error(message);
}

/**
 * 解析 protocol.go 里被标了 mirror 的常量。
 * 只接受字面量、整数，以及 `BasePath + "字面量"` 这种与本文件已声明常量的字符串拼接。
 */
export function readMirrors(text) {
  const constants = new Map();
  const mirrors = [];
  const lines = text.split(/\r?\n/);
  let section = null;
  let inBlock = false;

  const resolveValue = (raw, line) => {
    const literal = raw.match(/^"([^"]*)"$/);
    if (literal) return literal[1];
    if (/^-?\d+$/.test(raw)) return Number(raw);
    const concat = raw.match(/^(\w+) \+ "([^"]*)"$/);
    if (concat) {
      const base = constants.get(concat[1]);
      if (typeof base !== 'string') {
        fail(`${SOURCE}:${line}: ${concat[1]} 不是本文件已声明的字符串常量`);
      }
      return base + concat[2];
    }
    fail(`${SOURCE}:${line}: 镜像常量只接受字面量与 BasePath + "字面量" 拼接，收到 ${raw}`);
  };

  for (const [index, line] of lines.entries()) {
    const marker = line.match(/^\/\/\s*mirror\s+(.+)$/);
    if (marker) {
      section = marker[1]
        .trim()
        .split(/\s+/)
        .map((entry) => {
          const [target, group] = entry.split('.');
          if (!group || !SECTIONS[group])
            fail(`${SOURCE}:${index + 1}: 未知的 mirror 分组 ${entry}`);
          const declared = SECTIONS[group].target;
          if (declared && declared !== target) {
            fail(`${SOURCE}:${index + 1}: ${group} 只属于 ${declared}，不能挂在 ${target} 上`);
          }
          return { target, group };
        });
      continue;
    }
    if (!inBlock && /^const\s*\($/.test(line.trim())) {
      inBlock = true;
      continue;
    }
    if (inBlock && line.trim() === ')') {
      inBlock = false;
      section = null;
      continue;
    }
    const entry = line.match(/^\s*(?:const\s+)?([A-Z]\w*)\s*=\s*(.+?)\s*$/);
    if (entry) {
      const targets = section ?? [];
      let value;
      try {
        value = resolveValue(entry[2], index + 1);
      } catch (error) {
        // 非镜像的派生常量（例如 ExtensionOrigin）不在生成器关心的范围内，跳过。
        if (targets.length) throw error;
        value = undefined;
      }
      if (value !== undefined) {
        constants.set(entry[1], value);
        for (const { target, group } of targets) {
          mirrors.push({ target, group, name: entry[1], value });
        }
      }
      if (!inBlock) section = null;
      continue;
    }
    if (/^(\/\/|\s*$)/.test(line)) continue;
    if (inBlock) continue;
    if (section) fail(`${SOURCE}:${index + 1}: 镜像块里只允许常量声明，收到 ${line.trim()}`);
  }
  return { constants, mirrors };
}

function renderGroup({ group, keys }) {
  const { name, prefix, scalar } = SECTIONS[group];
  const [first] = keys;
  if (scalar) {
    const quoted = typeof first.value === 'number' ? `${first.value}` : `'${first.value}'`;
    return `export const ${name} = ${quoted};\n`;
  }
  const entries = keys.map(({ key, value }) => {
    const stripped = prefix && key.startsWith(prefix) ? key.slice(prefix.length) : key;
    return { key: stripped || key, rendered: typeof value === 'number' ? value : `'${value}'` };
  });
  // 数字分组不加 `as const`：那会把取值收窄成字面量类型，让调用方连普通的赋值都过不了类型检查。
  const assertion = entries.every(({ rendered }) => typeof rendered === 'number')
    ? ''
    : ' as const';
  const oneLine = `export const ${name} = { ${entries
    .map(({ key, rendered }) => `${key}: ${rendered}`)
    .join(', ')} }${assertion};\n`;
  if (oneLine.length <= 100) return oneLine;
  return `export const ${name} = {\n${entries
    .map(({ key, rendered }) => `  ${key}: ${rendered},`)
    .join('\n')}\n}${assertion};\n`;
}

/** 把镜像常量渲染成某个目标的 TypeScript 文件内容。 */
export function renderTarget(mirrors, target) {
  const groups = [];
  for (const mirror of mirrors) {
    if (mirror.target !== target) continue;
    let group = groups.find((candidate) => candidate.group === mirror.group);
    if (!group) groups.push((group = { group: mirror.group, keys: [] }));
    group.keys.push({ key: mirror.name, value: mirror.value });
  }
  return HEADER + '\n' + groups.map(renderGroup).join('\n');
}

/** 从扩展的固定公钥推导 Chrome 扩展 ID：公钥 DER 的 SHA-256 前 16 字节按 a-p 编码。 */
export function extensionIDFromKey(configText) {
  const key = configText.match(/FIXED_PUBLIC_KEY\s*=\s*\n?\s*'([^']+)'/);
  if (!key) fail(`${EXTENSION_CONFIG}: 找不到 FIXED_PUBLIC_KEY`);
  const digest = createHash('sha256').update(Buffer.from(key[1], 'base64')).digest();
  return [...digest.subarray(0, 16)]
    .map((byte) => byte.toString(16).padStart(2, '0'))
    .join('')
    .split('')
    .map((digit) => 'abcdefghijklmnop'[parseInt(digit, 16)])
    .join('');
}

/** 生成全部镜像文件内容（含扩展 ID 比对）。 */
export function buildMirrors(atRoot = root) {
  const { constants, mirrors } = readMirrors(readFileSync(join(atRoot, SOURCE), 'utf8'));
  const derivedID = extensionIDFromKey(readFileSync(join(atRoot, EXTENSION_CONFIG), 'utf8'));
  const declaredID = constants.get('ExtensionID');
  if (!declaredID) fail(`${SOURCE}: 必须声明 ExtensionID`);
  if (declaredID !== derivedID) {
    fail(
      `${SOURCE}: ExtensionID ${declaredID} 与 ${EXTENSION_CONFIG} 公钥推导结果 ${derivedID} 不一致`,
    );
  }
  return Object.fromEntries(
    Object.entries(TARGETS).map(([target, file]) => [file, renderTarget(mirrors, target)]),
  );
}

/** 检查镜像是否与 Go 侧一致；返回不一致或缺失的文件清单。 */
export function checkMirrors(atRoot = root) {
  const stale = [];
  for (const [file, expected] of Object.entries(buildMirrors(atRoot))) {
    const path = join(atRoot, file);
    if (!existsSync(path) || readFileSync(path, 'utf8') !== expected) stale.push(file);
  }
  return stale;
}

/** 重新生成全部镜像；返回写入的文件清单。 */
export function writeMirrors(atRoot = root) {
  const written = [];
  for (const [file, content] of Object.entries(buildMirrors(atRoot))) {
    const path = join(atRoot, file);
    if (existsSync(path) && readFileSync(path, 'utf8') === content) continue;
    writeFileSync(path, content);
    written.push(file);
  }
  return written;
}

function main(argv) {
  if (argv.length === 1 && argv[0] === '--check') {
    const stale = checkMirrors();
    if (stale.length) {
      fail(
        `TypeScript 镜像与 ${SOURCE} 不一致：${stale.join(', ')}（运行 node scripts/protocol.mjs --write）`,
      );
    }
    console.log(`[Protocol] 镜像与 ${SOURCE} 一致`);
    return;
  }
  if (argv.length === 1 && argv[0] === '--write') {
    const written = writeMirrors();
    console.log(
      `[Protocol] 已生成 ${written.length} 个镜像文件：${written.join(', ') || '无需改动'}`,
    );
    return;
  }
  fail('Usage: node scripts/protocol.mjs --check | --write');
}

if (resolve(process.argv[1] ?? '') === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
