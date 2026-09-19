import { spawn, execSync } from 'node:child_process';
import { resolve, join, delimiter } from 'node:path';
import { fileURLToPath } from 'node:url';
import { existsSync, rmSync } from 'node:fs';
import { setInterval, clearInterval } from 'node:timers';
const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const toolsDir = join(root, '.tools');
const wails3Exe = join(toolsDir, 'wails3' + (process.platform === 'win32' ? '.exe' : ''));
const devExitFile = join(toolsDir, '.dev-exit');

const args = process.argv.slice(2);
const isTopDev = args[0] === 'dev';

if (isTopDev && existsSync(devExitFile)) {
  try {
    rmSync(devExitFile, { force: true });
  } catch {
    // Ignore if file cannot be removed immediately
  }
}

const env = {
  ...process.env,
  PATH: toolsDir + delimiter + (process.env.PATH || ''),
  SHEEP_GET_DEV_EXIT_FILE: devExitFile,
};

const child = spawn(wails3Exe, args, {
  cwd: root,
  env,
  stdio: 'inherit',
});

let isExiting = false;
function cleanupChild() {
  if (isExiting || !child.pid) return;
  isExiting = true;
  if (existsSync(devExitFile)) {
    try {
      rmSync(devExitFile, { force: true });
    } catch {
      // Ignore
    }
  }
  try {
    if (process.platform === 'win32') {
      execSync(`taskkill /F /T /PID ${child.pid}`, { stdio: 'ignore' });
    } else {
      child.kill('SIGTERM');
    }
  } catch {
    // Process might already have exited
  }
}

if (isTopDev) {
  // Monitor intentional user shutdown signal emitted by App.Shutdown()
  const exitCheckInterval = setInterval(() => {
    if (isExiting) {
      clearInterval(exitCheckInterval);
      return;
    }
    if (existsSync(devExitFile)) {
      clearInterval(exitCheckInterval);
      cleanupChild();
      process.exit(0);
    }
  }, 500);

  child.on('exit', () => {
    clearInterval(exitCheckInterval);
    if (existsSync(devExitFile)) {
      try {
        rmSync(devExitFile, { force: true });
      } catch {
        // Ignore
      }
    }
    process.exit(0);
  });
} else {
  child.on('exit', (code, signal) => {
    if (signal) {
      process.exit(0);
    } else {
      process.exit(code ?? 0);
    }
  });
}

process.on('SIGINT', () => {
  cleanupChild();
  process.exit(0);
});

process.on('SIGTERM', () => {
  cleanupChild();
  process.exit(0);
});
