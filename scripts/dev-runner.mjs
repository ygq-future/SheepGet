import { spawn, execSync } from 'node:child_process';
import { resolve, join, delimiter } from 'node:path';
import { setInterval, clearInterval } from 'node:timers';
import { fileURLToPath } from 'node:url';
const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const toolsDir = join(root, '.tools');
const wails3Exe = join(toolsDir, 'wails3' + (process.platform === 'win32' ? '.exe' : ''));

const env = {
  ...process.env,
  PATH: toolsDir + delimiter + (process.env.PATH || ''),
};

const args = process.argv.slice(2);
const isTopDev = args[0] === 'dev';

const child = spawn(wails3Exe, args, {
  cwd: root,
  env,
  stdio: 'inherit',
});

if (isTopDev) {
  // Active supervision of sheep-get.exe lifecycle
  let appSeen = false;
  let exited = false;
  const checkInterval = setInterval(() => {
    if (exited) return;
    try {
      const out = execSync('tasklist /FI "IMAGENAME eq sheep-get.exe"', {
        stdio: ['ignore', 'pipe', 'ignore'],
      }).toString();
      const isRunning = out.includes('sheep-get.exe');
      if (isRunning) {
        appSeen = true;
      } else if (appSeen) {
        // App was running and has now exited (user closed window or quit)
        exited = true;
        clearInterval(checkInterval);
        try {
          if (process.platform === 'win32') {
            execSync(`taskkill /F /T /PID ${child.pid}`, { stdio: 'ignore' });
          } else {
            child.kill('SIGTERM');
          }
        } catch (error) {
          void error;
        }
        process.exit(0);
      }
    } catch (error) {
      void error;
    }
  }, 500);

  child.on('exit', () => {
    exited = true;
    clearInterval(checkInterval);
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
