#!/usr/bin/env node
// AIOS npm launcher.
//
// This file ships in the main @mooncodemaster/aios package. It does NOT contain any
// native code. Its only job is to locate the platform-specific native binary
// that npm installed via optionalDependencies, execute it with this process's
// argv/env, and faithfully forward exit code and signal behavior back to the
// caller.
//
// No network calls. No postinstall. No downloads. If npm did not install a
// platform sibling (e.g. --no-optional, unsupported platform, strict corporate
// mirror), we print a precise error naming the expected package and exit
// with a non-zero status — never silently degrade.
'use strict';

const { spawnSync } = require('node:child_process');
const path = require('node:path');
const fs = require('node:fs');

// Supported pairs map Node conventions (process.platform + process.arch) to
// the platform package name suffix. Keep this table in exact lockstep with
// npm/platforms/* and with build-npm.mjs — validate-npm.mjs enforces that.
const SUPPORTED = {
  'darwin-arm64': '@mooncodemaster/aios-darwin-arm64',
  'darwin-x64': '@mooncodemaster/aios-darwin-x64',
  'linux-arm64': '@mooncodemaster/aios-linux-arm64',
  'linux-x64': '@mooncodemaster/aios-linux-x64',
  'win32-x64': '@mooncodemaster/aios-win32-x64',
};

function die(msg, code = 1) {
  process.stderr.write(`aios: ${msg}\n`);
  process.exit(code);
}

function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = SUPPORTED[key];
  if (!pkg) {
    die(
      `unsupported platform ${key}.\n` +
        `  supported: ${Object.keys(SUPPORTED).join(', ')}\n` +
        `  if you need another platform, please open an issue:\n` +
        `    https://github.com/MoonCodeMaster/AIOS/issues`,
      1,
    );
  }

  // Resolve the sibling package via its package.json (not its main field)
  // so we get a stable anchor regardless of what the sibling exports.
  let siblingPkgJson;
  try {
    siblingPkgJson = require.resolve(`${pkg}/package.json`);
  } catch (err) {
    die(
      `the platform package "${pkg}" is not installed.\n` +
        `  this usually means npm install ran with --no-optional, --omit=optional,\n` +
        `  or a corporate registry mirror is missing the sibling package.\n` +
        `  try:    npm install -g @mooncodemaster/aios --include=optional\n` +
        `  or:     npm install -g @mooncodemaster/aios --omit=\n` +
        `  original error: ${err.message}`,
      1,
    );
  }

  const siblingDir = path.dirname(siblingPkgJson);
  const binName = process.platform === 'win32' ? 'aios.exe' : 'aios';
  const binPath = path.join(siblingDir, 'bin', binName);

  if (!fs.existsSync(binPath)) {
    die(
      `platform package "${pkg}" is installed but the native binary is missing.\n` +
        `  expected at: ${binPath}\n` +
        `  this indicates a corrupted install. please reinstall:\n` +
        `    npm uninstall -g @mooncodemaster/aios && npm install -g @mooncodemaster/aios`,
      1,
    );
  }

  return binPath;
}

function main() {
  const bin = resolveBinary();
  const args = process.argv.slice(2);

  // spawnSync with stdio:'inherit' forwards stdin (interactive prompts like
  // `aios new`), stdout, and stderr transparently. Signal forwarding is the
  // default in Node — SIGINT/SIGTERM sent to this process propagate to the
  // child's process group.
  const result = spawnSync(bin, args, {
    stdio: 'inherit',
    env: process.env,
    windowsHide: false,
  });

  if (result.error) {
    // ENOENT / EACCES / ETXTBSY etc. — surface verbatim; do not rewrite.
    die(`failed to execute native binary: ${result.error.message}`);
  }

  // If the child was terminated by a signal, re-raise it on this process so
  // the exit status the caller sees is indistinguishable from running the
  // binary directly. If that is not possible (Windows, signal not catchable),
  // fall back to the POSIX "128 + signal number" convention.
  if (result.signal) {
    try {
      process.kill(process.pid, result.signal);
    } catch {
      // ignore; fall through to exit code
    }
    // In case the above returned (rare), still exit with something sensible.
    const signum = require('node:os').constants.signals[result.signal];
    process.exit(typeof signum === 'number' ? 128 + signum : 1);
  }

  process.exit(result.status ?? 1);
}

main();
