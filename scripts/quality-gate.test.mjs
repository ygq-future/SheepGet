import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync, mkdirSync, copyFileSync, readFileSync } from 'node:fs';
import { resolve, join, sep, dirname } from 'node:path';
import { spawnSync } from 'node:child_process';
import { ESLint } from '../frontend/node_modules/eslint/lib/api.js';
import * as prettier from '../frontend/node_modules/prettier/index.mjs';
import { run } from './process.mjs';
import { readInfo, versionFiles } from './version.mjs';

const root = resolve(import.meta.dirname, '..');
const linter = new ESLint({ cwd: join(root, 'frontend') });
const lint = (text) => linter.lintText(text, { filePath: join(root, 'frontend/src/App.tsx') });
const exec = (command, args, options = {}) =>
  spawnSync(command, args, { encoding: 'utf8', ...options });

test('process wrapper propagates child failure and missing executable', () => {
  assert.throws(() => run(process.execPath, ['-e', 'process.exit(7)']), /exited with 7/);
  assert.throws(() => run('sheepget-quality-missing-tool', []));
});

test('lint detects hooks, unsafe types, promises and deprecated calls', async () => {
  const cases = [
    ['export const value: any = 1;', '@typescript-eslint/no-explicit-any'],
    [
      'import { useState } from "react"; export function Example({ enabled }: { enabled: boolean }) { if (enabled) useState(0); return null; }',
      'react-hooks/rules-of-hooks',
    ],
    ['export function f() { Promise.resolve(1); }', '@typescript-eslint/no-floating-promises'],
    [
      '/** @deprecated use modern */\nfunction old() {}\nexport function modern() { old(); }',
      '@typescript-eslint/no-deprecated',
    ],
  ];
  for (const [source, rule] of cases) {
    const results = await lint(source);
    assert.ok(
      results.flatMap((r) => r.messages).some((m) => m.ruleId === rule && m.severity === 2),
      rule,
    );
  }
  const valid = await lint('export const value: number = 1;');
  assert.equal(valid[0].errorCount, 0);
});

test('TypeScript 7 rejects semantic errors and accepts valid code', () => {
  const base = join(root, '.tools');
  const fixture = mkdtempSync(join(base, 'typescript-probe-'));
  try {
    const file = join(fixture, 'probe.ts');
    const cli = join(root, 'frontend/node_modules/@typescript/native/bin/tsc');
    writeFileSync(
      join(fixture, 'tsconfig.json'),
      JSON.stringify({ compilerOptions: { noEmit: true, strict: true }, files: ['probe.ts'] }),
    );
    writeFileSync(file, 'const value: number = "wrong"; export { value };');
    const args = [cli, '-p', join(fixture, 'tsconfig.json')];
    const invalid = exec(process.execPath, args);
    assert.notEqual(invalid.status, 0);
    assert.match(invalid.stdout + invalid.stderr, /TS2322/);
    writeFileSync(file, 'const value: number = 1; export { value };');
    const valid = exec(process.execPath, args);
    assert.equal(valid.status, 0, valid.stdout + valid.stderr);
  } finally {
    assert.ok(resolve(fixture).startsWith(resolve(base) + sep));
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('Prettier loads Tailwind stylesheet and sorts Tailwind classes', async () => {
  const cli = join(root, 'frontend/node_modules/prettier/bin/prettier.cjs');
  for (const cwd of [root, join(root, 'frontend')]) {
    const result = exec(process.execPath, [cli, '--check', join(root, 'frontend/package.json')], {
      cwd,
    });
    assert.equal(result.status, 0, result.stdout + result.stderr);
  }
  const previous = process.cwd();
  process.chdir(join(root, 'frontend'));
  try {
    const config = await prettier.resolveConfig('src/App.tsx');
    assert.equal(config.tailwindStylesheet, './src/style.css');
    const source = 'export const el = <div className="p-4 flex" />;\n';
    const options = { ...config, filepath: 'src/App.tsx' };
    assert.equal(await prettier.check(source, options), false);
    const formatted = await prettier.format(source, options);
    assert.match(formatted, /flex p-4/);
    assert.equal(await prettier.check(formatted, options), true);
  } finally {
    process.chdir(previous);
  }
});

test('commit message interface accepts and rejects actual messages', () => {
  const cli = join(root, 'node_modules/@commitlint/cli/cli.js');
  assert.equal(
    exec(process.execPath, [cli], { cwd: root, input: 'docs: quality contract\n' }).status,
    0,
  );
  assert.notEqual(
    exec(process.execPath, [cli], { cwd: root, input: 'invalid message\n' }).status,
    0,
  );
});

test('isolated Go diagnostics and failing tests return failure', () => {
  const base = join(root, '.tools');
  mkdirSync(base, { recursive: true });
  const fixture = mkdtempSync(join(base, 'quality-probe-'));
  try {
    writeFileSync(join(fixture, 'go.mod'), 'module qualityprobe\n\ngo 1.27.1\n');
    writeFileSync(
      join(fixture, 'probe.go'),
      'package qualityprobe\n\nimport "io/ioutil"\n\nfunc Read() ([]byte, error) { return ioutil.ReadFile("sample") }\n',
    );
    const analyzer = join(
      root,
      '.tools/golangci-lint' + (process.platform === 'win32' ? '.exe' : ''),
    );
    const diagnostics = exec(analyzer, ['run', '--config', join(root, '.golangci.yml'), '.'], {
      cwd: fixture,
    });
    assert.notEqual(diagnostics.status, 0);
    assert.match(diagnostics.stdout + diagnostics.stderr, /SA1019/);
    writeFileSync(
      join(fixture, 'probe.go'),
      '// Package qualityprobe verifies diagnostic propagation.\npackage qualityprobe\n',
    );
    writeFileSync(
      join(fixture, 'probe_test.go'),
      'package qualityprobe\n\nimport "testing"\n\nfunc TestFailure(t *testing.T) { t.Fatal("intentional probe") }\n',
    );
    assert.notEqual(exec('go', ['test', '.'], { cwd: fixture }).status, 0);
    writeFileSync(
      join(fixture, 'probe_test.go'),
      'package qualityprobe\n\nimport "testing"\n\nfunc TestSuccess(t *testing.T) {}\n',
    );
    assert.equal(exec('go', ['test', '.'], { cwd: fixture }).status, 0);
  } finally {
    assert.ok(resolve(fixture).startsWith(resolve(base) + sep));
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('version check rejects drifted copies and --set rewrites every source', () => {
  const base = join(root, '.tools');
  mkdirSync(base, { recursive: true });
  const fixture = mkdtempSync(join(base, 'version-probe-'));
  const cli = join(fixture, 'scripts/version.mjs');
  const read = (file) => readFileSync(join(fixture, file), 'utf8');
  try {
    for (const file of [...versionFiles(), 'scripts/version.mjs']) {
      mkdirSync(dirname(join(fixture, file)), { recursive: true });
      copyFileSync(join(root, file), join(fixture, file));
    }
    assert.equal(exec(process.execPath, [cli, '--check']).status, 0);

    const { version } = readInfo(fixture);
    const packagePath = join(fixture, 'package.json');
    writeFileSync(
      packagePath,
      read('package.json').replace(`"version": "${version}"`, '"version": "9.9.9"'),
    );
    const drifted = exec(process.execPath, [cli, '--check']);
    assert.notEqual(drifted.status, 0);
    assert.match(drifted.stderr, /package\.json:.*9\.9\.9/);

    const rejected = exec(process.execPath, [cli, '--set', '1.0']);
    assert.notEqual(rejected.status, 0);
    assert.match(rejected.stderr, /X\.Y\.Z/);
    assert.match(read('package.json'), /"version": "9\.9\.9"/);

    const target = '2.5.0';
    const applied = exec(process.execPath, [cli, '--set', target]);
    assert.equal(applied.status, 0, applied.stdout + applied.stderr);
    assert.equal(exec(process.execPath, [cli, '--check']).status, 0);
    assert.equal(readInfo(fixture).version, target);
    assert.match(read('build/windows/info.json'), /"ProductVersion": "2\.5\.0"/);
    assert.match(read('internal/version/version.go'), /Version = "2\.5\.0"/);
    assert.match(read('extension/wxt.config.ts'), /version: '2\.5\.0'/);
    assert.match(read('extension/entrypoints/popup/App.tsx'), />v2\.5\.0<\/span>/);
    assert.match(read('.github/workflows/release.yml'), /default: 'v2\.5\.0'/);
    assert.match(read('README.md'), /SheepGet_2\.5\.0_x64-setup\.exe/);
    assert.equal(readInfo(root).version, version);
  } finally {
    assert.ok(resolve(fixture).startsWith(resolve(base) + sep));
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('frozen dependency validation rejects manifest drift', () => {
  const base = join(root, '.tools');
  const fixture = mkdtempSync(join(base, 'lock-probe-'));
  try {
    copyFileSync(join(root, 'bun.lock'), join(fixture, 'bun.lock'));
    writeFileSync(
      join(fixture, 'package.json'),
      JSON.stringify({
        name: 'sheep-get-workspace',
        devDependencies: {
          '@commitlint/cli': '21.2.1',
          '@commitlint/config-conventional': '21.2.2',
        },
      }),
    );
    const result = exec('bun', ['install', '--frozen-lockfile', '--dry-run', '--ignore-scripts'], {
      cwd: fixture,
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /lockfile.*frozen/);
  } finally {
    assert.ok(resolve(fixture).startsWith(resolve(base) + sep));
    rmSync(fixture, { recursive: true, force: true });
  }
});

test('protocol mirror check rejects drift and a key/ID mismatch', () => {
  const base = join(root, '.tools');
  mkdirSync(base, { recursive: true });
  const fixture = mkdtempSync(join(base, 'protocol-probe-'));
  const cli = join(fixture, 'scripts/protocol.mjs');
  const source = join(fixture, 'internal/protocol/protocol.go');
  const config = join(fixture, 'extension/wxt.config.ts');
  const mirror = join(fixture, 'extension/lib/protocol.generated.ts');
  const read = (path) => readFileSync(path, 'utf8');
  const check = () => exec(process.execPath, [cli, '--check']);
  try {
    for (const file of [
      'internal/protocol/protocol.go',
      'extension/wxt.config.ts',
      'extension/lib/protocol.generated.ts',
      'frontend/src/lib/protocol.generated.ts',
      'scripts/protocol.mjs',
    ]) {
      mkdirSync(dirname(join(fixture, file)), { recursive: true });
      copyFileSync(join(root, file), join(fixture, file));
    }
    assert.equal(check().status, 0);

    // 改一处线上事实却没同步镜像：门禁阶段必须失败
    writeFileSync(
      source,
      read(source).replace('const BasePath = "/api/v1"', 'const BasePath = "/api/v2"'),
    );
    const drifted = check();
    assert.notEqual(drifted.status, 0);
    assert.match(drifted.stderr, /protocol\.generated\.ts/);

    // 重新生成后镜像跟着 Go 侧走
    assert.equal(exec(process.execPath, [cli, '--write']).status, 0);
    assert.equal(check().status, 0);
    assert.match(read(mirror), /'\/api\/v2\/ping'/);

    // 换了扩展公钥却忘了改 ExtensionID：门禁阶段同样必须失败
    writeFileSync(config, read(config).replace("IDAQAB';", "IDAQAC';"));
    const idMismatch = check();
    assert.notEqual(idMismatch.status, 0);
    assert.match(idMismatch.stderr, /ExtensionID/);
  } finally {
    assert.ok(resolve(fixture).startsWith(resolve(base) + sep));
    rmSync(fixture, { recursive: true, force: true });
  }
});
