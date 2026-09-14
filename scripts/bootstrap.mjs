import { chmodSync, existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import tools from './quality-tools.json' with { type: 'json' };
import { run } from './process.mjs';
const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
process.chdir(root);
try {
  if (process.versions.node !== tools.node) throw new Error(`Node ${tools.node} required`);
  if (run('bun', ['--version']).trim() !== tools.bun) throw new Error(`Bun ${tools.bun} required`);
  run('bun', ['install', '--frozen-lockfile']);
  run('bun', ['install', '--frozen-lockfile'], { cwd: resolve('frontend') });
  const env = { ...process.env, GOBIN: resolve('.tools') };
  run(
    'go',
    ['install', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v' + tools.golangci],
    { env, stdio: 'inherit' },
  );
  run('go', ['install', 'github.com/wailsapp/wails/v3/cmd/wails3@v' + tools.wails3], {
    env,
    stdio: 'inherit',
  });
  run('go', ['install', 'github.com/rhysd/actionlint/cmd/actionlint@v' + tools.actionlint], {
    env,
    stdio: 'inherit',
  });
  if (!process.argv.includes('--ci')) {
    for (const [name, expected] of [
      ['pre-commit', '#!/bin/sh\nnode scripts/quality-gate.mjs'],
      ['commit-msg', '#!/bin/sh\nbun x commitlint --edit "$1"'],
    ]) {
      const path = '.git/hooks/' + name;
      if (existsSync(path) && readFileSync(path, 'utf8').replaceAll('\r', '').trim() !== expected) {
        throw new Error('Existing custom hook requires explicit integration: ' + path);
      }
      chmodSync('.githooks/' + name, 0o755);
    }
    run('git', ['config', '--local', 'core.hooksPath', '.githooks']);
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
