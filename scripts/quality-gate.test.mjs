import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync, mkdirSync, copyFileSync } from 'node:fs';
import { resolve, join, sep } from 'node:path';
import { spawnSync } from 'node:child_process';
import { ESLint } from '../frontend/node_modules/eslint/lib/api.js';
import * as prettier from '../frontend/node_modules/prettier/index.mjs';
import ts from '../frontend/node_modules/typescript/lib/typescript.js';
import { run } from './process.mjs';

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

test('TypeScript reports actual semantic errors', () => {
  const file = 'quality-probe.ts';
  const source = 'const value: number = "wrong"; export { value };';
  const options = { noEmit: true, strict: true };
  const host = ts.createCompilerHost(options);
  const original = host.getSourceFile;
  host.getSourceFile = (name, ...rest) =>
    name === file
      ? ts.createSourceFile(name, source, ts.ScriptTarget.Latest)
      : original(name, ...rest);
  const program = ts.createProgram([file], options, host);
  assert.ok(ts.getPreEmitDiagnostics(program).some((d) => d.code === 2322));
});

test('Prettier loads Tailwind stylesheet and sorts cn classes', async () => {
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
    const source = 'export const classes = cn("p-4 flex");\n';
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
