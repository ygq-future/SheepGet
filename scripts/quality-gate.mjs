import { spawnSync } from "node:child_process";

const isWindows = process.platform === "win32";

function runCmd(cmd, args) {
  return spawnSync(cmd, args, { stdio: "inherit", shell: isWindows });
}

const steps = [
  {
    name: "Go Formatting",
    run: () => {
      const res = spawnSync("gofmt", ["-l", "."], { encoding: "utf-8" });
      if (res.status !== 0) return false;
      const unformatted = res.stdout ? res.stdout.trim() : "";
      if (unformatted.length > 0) {
        process.stderr.write(`Unformatted files found:\n${unformatted}\n`);
        return false;
      }
      return true;
    },
  },
  {
    name: "Go Vet",
    run: () => runCmd("go", ["vet", "./..."]).status === 0,
  },
  {
    name: "Go Test",
    run: () => runCmd("go", ["test", "./..."]).status === 0,
  },
  {
    name: "Frontend TypeCheck",
    run: () => runCmd("bun", ["--cwd", "frontend", "typecheck"]).status === 0,
  },
  {
    name: "Frontend Lint",
    run: () => runCmd("bun", ["--cwd", "frontend", "lint"]).status === 0,
  },
  {
    name: "Frontend Format Check",
    run: () => runCmd("bun", ["--cwd", "frontend", "format:check"]).status === 0,
  },
];

let failed = false;

for (const step of steps) {
  process.stdout.write(`\n[Quality Gate] Running: ${step.name}...\n`);
  const ok = step.run();
  if (!ok) {
    process.stderr.write(`[Quality Gate] FAILED: ${step.name}\n`);
    failed = true;
    break;
  }
  process.stdout.write(`[Quality Gate] PASSED: ${step.name}\n`);
}

if (failed) {
  process.exit(1);
} else {
  process.stdout.write(
    "\n========================================\n[Quality Gate] ALL CHECKS PASSED (Clean)\n========================================\n"
  );
}
