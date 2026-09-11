import { spawnSync } from 'node:child_process';

export function run(command, args, options = {}) {
  const { quiet, ...spawnOptions } = options;
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    stdio: ['pipe', 'pipe', 'pipe'],
    ...spawnOptions,
    shell: false,
  });
  if (result.stdout && (!quiet || result.status !== 0)) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
  if (result.error) throw result.error;
  if (result.status !== 0)
    throw new Error(`${command} exited with ${result.status ?? result.signal}`);
  return result.stdout ?? '';
}
