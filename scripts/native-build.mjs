import { cpSync, mkdtempSync, readdirSync, mkdirSync, rmSync } from 'node:fs';
import { resolve, join, sep, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { run } from './process.mjs';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const base = join(root, '.tools');
mkdirSync(base, { recursive: true });
const workspace = mkdtempSync(join(base, 'native-build-'));
if (!resolve(workspace).startsWith(resolve(base) + sep)) {
  throw new Error('Invalid build workspace');
}
try {
  for (const entry of readdirSync(root)) {
    if (['.git', '.tools', 'node_modules'].includes(entry)) continue;
    cpSync(join(root, entry), join(workspace, entry), {
      recursive: true,
      filter: (source) => {
        const path = relative(root, source).replaceAll('\\', '/');
        return !path.split('/').includes('node_modules') && !path.startsWith('build/bin');
      },
    });
  }
  run(
    join(base, 'wails' + (process.platform === 'win32' ? '.exe' : '')),
    [
      'build',
      '-skipbindings',
      '-skipembedcreate',
      '-m',
      '-nosyncgomod',
      '-s',
      ...(process.platform === 'linux' ? ['-tags', 'webkit2_41'] : []),
    ],
    { cwd: workspace, stdio: 'inherit' },
  );
  cpSync(join(workspace, 'build/bin'), join(root, 'build/bin'), { recursive: true });
} finally {
  rmSync(workspace, { recursive: true, force: true });
}
