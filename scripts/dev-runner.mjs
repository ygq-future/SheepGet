import { spawn } from 'node:child_process';
import { resolve, join, delimiter } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const toolsDir = join(root, '.tools');
const wails3Exe = join(toolsDir, 'wails3' + (process.platform === 'win32' ? '.exe' : ''));

const env = {
  ...process.env,
  PATH: toolsDir + delimiter + (process.env.PATH || ''),
};

const args = process.argv.slice(2);
const child = spawn(wails3Exe, args, {
  cwd: root,
  env,
  stdio: 'inherit',
});

child.on('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 0);
  }
});
