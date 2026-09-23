import { readFileSync, readdirSync, existsSync, mkdirSync, rmSync } from 'node:fs';
import { resolve, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import tools from './quality-tools.json' with { type: 'json' };
import { run } from './process.mjs';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
process.chdir(root);
const exe = (name) => join(root, '.tools', name + (process.platform === 'win32' ? '.exe' : ''));
const node = (file, args = [], options = {}) =>
  run(process.execPath, ['--throw-deprecation', file, ...args], options);
const commitlint = 'node_modules/@commitlint/cli/cli.js';
const args = process.argv.slice(2);
const git = (...values) => {
  const result = spawnSync('git', values, { encoding: 'utf8' });
  if (result.status !== 0) throw new Error(result.stderr || 'Git command failed');
  return result.stdout;
};

function files(dir, predicate) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (
      ['node_modules', '.git', '.tools', 'dist', 'wailsjs', 'bindings', 'bin'].includes(entry.name)
    )
      return [];
    const path = join(dir, entry.name);
    return entry.isDirectory() ? files(path, predicate) : predicate(path) ? [path] : [];
  });
}

function fingerprint() {
  const paths = git('ls-files', '--cached', '--others', '--exclude-standard', '-z')
    .split('\0')
    .filter(Boolean);
  return new Map(
    paths
      .filter(existsSync)
      .map((path) => [path, createHash('sha256').update(readFileSync(path)).digest('hex')]),
  );
}

function toolVersions() {
  if (process.versions.node !== tools.node) throw new Error(`Node ${tools.node} required`);
  for (const [command, values, expected] of [
    ['bun', ['--version'], tools.bun],
    ['go', ['version'], 'go' + tools.go],
    [exe('golangci-lint'), ['version'], 'version ' + tools.golangci],
    [exe('wails3'), ['tool', 'buildinfo'], tools.wails3],
    [exe('actionlint'), ['-version'], tools.actionlint],
  ]) {
    if (!run(command, values).includes(expected)) throw new Error(`Unexpected version: ${command}`);
  }
}

function goFormat() {
  const paths = files(root, (path) => path.endsWith('.go'));
  if (!paths.length) throw new Error('No Go source discovered');
  for (let offset = 0; offset < paths.length; offset += 100) {
    if (run('gofmt', ['-l', ...paths.slice(offset, offset + 100)]).trim()) {
      throw new Error('Go formatting failed');
    }
  }
}

function ownedPackages() {
  const entries = run('go', ['list', '-mod=readonly', '-f', '{{.ImportPath}}|{{.Dir}}', './...'])
    .trim()
    .split('\n');
  const packages = entries
    .filter((entry) => !/[\\/]node_modules[\\/]/.test(entry))
    .map((entry) => './' + relative(root, entry.trim().split('|')[1]).replaceAll('\\', '/'));
  if (!packages.length) throw new Error('No owned Go packages discovered');
  return packages;
}

function tests() {
  const goTests = files(root, (path) => path.endsWith('_test.go'));
  const webTests = files(join(root, 'frontend/src'), (path) =>
    /\.(test|spec)\.[jt]sx?$/.test(path),
  );
  const bootstrap = JSON.parse(readFileSync('scripts/test-scope.json', 'utf8'));
  if ((!goTests.length || !webTests.length) && !bootstrap.scaffoldOnly) {
    throw new Error('Both Go and frontend behavior tests are required');
  }
  run('go', ['test', '-mod=readonly', ...ownedPackages()]);
  if (webTests.length)
    node('node_modules/vitest/vitest.mjs', ['run'], { cwd: join(root, 'frontend') });
  else console.log('PARTIAL: frontend behavior tests pending (scaffold-only contract)');
  if (!goTests.length) console.log('PARTIAL: Go behavior tests pending (scaffold-only contract)');
  run('bun', ['test'], { cwd: join(root, 'extension') });
}

const stages = {
  format() {
    goFormat();
    node('frontend/node_modules/prettier/bin/prettier.cjs', [
      '--check',
      'scripts',
      '*.json',
      '*.mjs',
      '.golangci.yml',
      '.github',
      'README.md',
      'AGENTS.md',
      'docs/agents/quality.md',
      'extension/**/*.{ts,tsx,json,html}',
    ]);
    node('node_modules/prettier/bin/prettier.cjs', ['--check', '.'], {
      cwd: join(root, 'frontend'),
    });
  },
  version() {
    node('scripts/version.mjs', ['--check']);
  },
  protocol() {
    node('scripts/protocol.mjs', ['--check']);
  },
  dependencies() {
    run('go', ['mod', 'verify']);
    run('go', ['mod', 'tidy', '-diff']);
    run('bun', ['install', '--frozen-lockfile', '--dry-run', '--ignore-scripts'], { quiet: true });
    run('bun', ['install', '--frozen-lockfile', '--dry-run', '--ignore-scripts'], {
      cwd: join(root, 'frontend'),
      quiet: true,
    });
    run('bun', ['install', '--frozen-lockfile', '--dry-run', '--ignore-scripts'], {
      cwd: join(root, 'extension'),
      quiet: true,
    });
  },
  diagnostics() {
    run(exe('actionlint'), ['-shellcheck=', '-pyflakes=']);
    node('frontend/node_modules/eslint/bin/eslint.js', [
      'scripts',
      'eslint.config.mjs',
      '--max-warnings',
      '0',
    ]);
    node('node_modules/@typescript/native/bin/tsc', ['--noEmit', '-p', 'tsconfig.json'], {
      cwd: join(root, 'frontend'),
    });
    node('node_modules/eslint/bin/eslint.js', ['.', '--max-warnings', '0'], {
      cwd: join(root, 'frontend'),
    });
    run('bun', ['run', 'compile'], {
      cwd: join(root, 'extension'),
    });
  },
  frontendBuild() {
    node('node_modules/vite/bin/vite.js', ['build'], { cwd: join(root, 'frontend') });
  },
  extensionBuild() {
    run('bun', ['run', 'build'], { cwd: join(root, 'extension') });
  },
  goAnalysis() {
    run(exe('golangci-lint'), ['config', 'verify']);
    run(exe('golangci-lint'), ['run', ...ownedPackages()]);
  },
  tests,
  build() {
    const target = 'build/bin/quality-app' + (process.platform === 'win32' ? '.exe' : '');
    mkdirSync('build/bin', { recursive: true });
    try {
      run('go', ['build', '-mod=readonly', '-o', target, '.']);
    } finally {
      rmSync(target, { force: true });
    }
  },
  infrastructure() {
    node('--test', ['scripts/quality-gate.test.mjs']);
  },
};

try {
  if (args[0] === '--message' && args.length === 2) {
    node(commitlint, ['--edit', resolve(args[1])]);
  } else if (args[0] === '--range' && args.length === 3) {
    const commits = git('rev-list', args[1] === 'ROOT' ? args[2] : args[1] + '..' + args[2])
      .trim()
      .split('\n')
      .filter(Boolean);
    if (!commits.length) throw new Error('Empty commit range');
    for (const commit of commits)
      node(commitlint, [], { input: git('show', '-s', '--format=%B', commit) });
  } else {
    if (
      args.length &&
      !(args.length === 1 && ['--staged', '--ci'].includes(args[0])) &&
      !(args.length === 2 && args[0] === '--stage' && Object.hasOwn(stages, args[1]))
    ) {
      throw new Error(
        'Usage: quality [--staged | --ci | --stage NAME | --message FILE | --range BASE HEAD]',
      );
    }
    if (
      args[0] === '--staged' &&
      (git('diff', '--name-only').trim() ||
        git('ls-files', '--others', '--exclude-standard').trim())
    ) {
      throw new Error(
        'Pre-commit requires an index matching the working tree; review/stage all intended changes first. No auto-staging.',
      );
    }
    toolVersions();
    const before = fingerprint();
    let stageError;
    try {
      const selected =
        args[0] === '--stage' ? [[args[1], stages[args[1]]]] : Object.entries(stages);
      for (const [name, action] of selected) {
        console.log(`[Quality] ${name}`);
        action();
      }
      if (args[0] === '--ci') node('scripts/native-build.mjs');
    } catch (error) {
      stageError = error;
    }
    {
      const after = fingerprint();
      if (JSON.stringify([...before].sort()) !== JSON.stringify([...after].sort())) {
        throw new Error('Quality check changed maintained files; inspect the working tree');
      }
    }
    if (stageError) throw stageError;
    console.log(
      'Declared checks passed; cross-platform CI and real-device results are reported separately from local verification.',
    );
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
