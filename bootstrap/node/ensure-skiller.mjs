#!/usr/bin/env node
import { accessSync, constants, existsSync } from "node:fs";
import { homedir, platform } from "node:os";
import { delimiter, join } from "node:path";
import { spawnSync } from "node:child_process";

const EXIT_MISSING = 10;
const EXIT_TOO_OLD = 11;
const EXIT_INVALID = 12;
const EXIT_EXEC = 13;
const EXIT_INSTALL = 20;
const DEFAULT_MIN_VERSION = "0.0.0";
const DEFAULT_INSTALL_COMMAND =
  "curl -fsSL https://raw.githubusercontent.com/mostlydev/skiller/master/scripts/install.sh | sh";

function main() {
  const opts = parseArgs(process.argv.slice(2), process.env);
  const result = ensureSkiller(opts);
  if (result.ok) {
    console.log(`${result.path} ${result.version}`);
    return;
  }
  if (opts.allowDownload) {
    const installed = runInstall(opts.installCommand);
    if (!installed.ok) {
      console.error(`skiller install command failed: ${opts.installCommand}`);
      process.exit(EXIT_INSTALL);
    }
    const retry = ensureSkiller({ ...opts, allowDownload: false });
    if (retry.ok) {
      console.log(`${retry.path} ${retry.version}`);
      return;
    }
    console.error(retry.message);
    console.error(`Install command: ${opts.installCommand}`);
    process.exit(retry.code);
  }
  console.error(result.message);
  console.error(`Install command: ${opts.installCommand}`);
  process.exit(result.code);
}

function parseArgs(args, env) {
  const opts = {
    minVersion: env.SKILLER_MIN_VERSION || DEFAULT_MIN_VERSION,
    allowDownload: env.SKILLER_BOOTSTRAP_ALLOW_DOWNLOAD === "1",
    installCommand: env.SKILLER_BOOTSTRAP_INSTALL_COMMAND || DEFAULT_INSTALL_COMMAND,
  };
  for (let i = 0; i < args.length; i += 1) {
    switch (args[i]) {
      case "--min-version":
        opts.minVersion = args[++i] || "";
        break;
      case "--allow-download":
        opts.allowDownload = true;
        break;
      case "--install-command":
        opts.installCommand = args[++i] || "";
        break;
      default:
        throw new Error(`unknown argument ${args[i]}`);
    }
  }
  return opts;
}

function ensureSkiller(opts) {
  const binary = findBinary(process.env);
  if (!binary) {
    return { ok: false, code: EXIT_MISSING, message: "skiller binary not found" };
  }
  const version = readVersion(binary);
  if (!version.ok) {
    return version;
  }
  const cmp = compareVersions(version.version, opts.minVersion);
  if (cmp === null) {
    return { ok: false, code: EXIT_INVALID, message: `invalid skiller version: ${version.version}` };
  }
  if (cmp < 0) {
    return {
      ok: false,
      code: EXIT_TOO_OLD,
      message: `skiller ${version.version} is older than required ${opts.minVersion}`,
    };
  }
  return { ok: true, path: binary, version: version.version };
}

function findBinary(env) {
  if (env.SKILLER_BIN && isExecutable(env.SKILLER_BIN)) {
    return env.SKILLER_BIN;
  }
  for (const dir of (env.PATH || "").split(delimiter)) {
    if (!dir) continue;
    for (const name of executableNames()) {
      const candidate = join(dir, name);
      if (isExecutable(candidate)) return candidate;
    }
  }
  const local = join(homedir(), ".local", "bin", executableNames()[0]);
  if (isExecutable(local)) return local;
  return "";
}

function executableNames() {
  return platform() === "win32" ? ["skiller.exe", "skiller"] : ["skiller"];
}

function isExecutable(path) {
  try {
    if (!existsSync(path)) return false;
    accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function readVersion(binary) {
  const child = spawnSync(binary, ["version", "--json"], { encoding: "utf8" });
  if (child.status !== 0) {
    return { ok: false, code: EXIT_EXEC, message: `failed to run ${binary} version --json` };
  }
  try {
    const parsed = JSON.parse(child.stdout);
    if (!parsed.version || typeof parsed.version !== "string") {
      return { ok: false, code: EXIT_INVALID, message: "skiller version JSON has no version" };
    }
    return { ok: true, version: parsed.version };
  } catch {
    return { ok: false, code: EXIT_INVALID, message: "skiller version JSON is invalid" };
  }
}

function compareVersions(found, minimum) {
  const a = parseVersion(found);
  const b = parseVersion(minimum);
  if (!a || !b) return null;
  for (let i = 0; i < 3; i += 1) {
    if (a[i] > b[i]) return 1;
    if (a[i] < b[i]) return -1;
  }
  return 0;
}

function parseVersion(value) {
  const match = String(value).trim().match(/^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?/);
  if (!match) return null;
  return [Number(match[1]), Number(match[2] || 0), Number(match[3] || 0)];
}

function runInstall(command) {
  const child = spawnSync(command, { shell: true, stdio: "inherit" });
  return { ok: child.status === 0 };
}

main();
