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

if (isTopDev) {
  if (existsSync(devExitFile)) {
    try {
      rmSync(devExitFile, { force: true });
    } catch {
      // Ignore if file cannot be removed immediately
    }
  }
  // 启动前若检测到 9245 端口被上次残留脱钩的孤儿进程占用，自动先行释放
  killPortOccupant(9245);
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
  if (isExiting) return;
  isExiting = true;
  if (existsSync(devExitFile)) {
    try {
      rmSync(devExitFile, { force: true });
    } catch {
      // Ignore
    }
  }
  try {
    if (child && child.pid) {
      if (process.platform === 'win32') {
        execSync(`taskkill /F /T /PID ${child.pid}`, { stdio: 'ignore' });
      } else {
        child.kill('SIGTERM');
      }
    }
  } catch {
    // Process might already have exited
  }
  // 针对 Windows 进程树脱钩导致的孤儿 Vite 监听进程进行兜底清理
  killPortOccupant(9245);
}

function killPortOccupant(port) {
  if (process.platform !== 'win32') return;
  try {
    const netstat = execSync('netstat -ano', {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    });
    for (const line of netstat.split('\n')) {
      if (line.includes(`:${port}`) && line.includes('LISTENING')) {
        const parts = line.trim().split(/\s+/);
        const pid = parts[parts.length - 1];
        if (pid && /^\d+$/.test(pid) && pid !== String(process.pid)) {
          try {
            execSync(`taskkill /F /PID ${pid}`, { stdio: 'ignore' });
          } catch {
            // Ignore if process already exited
          }
        }
      }
    }
  } catch {
    // Ignore netstat errors
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
    cleanupChild();
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
