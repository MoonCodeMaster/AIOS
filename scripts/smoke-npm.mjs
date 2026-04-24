#!/usr/bin/env node
// smoke-npm.mjs — local end-to-end smoke test of the npm distribution.
//
// Steps:
//   1. Require dist/npm/ to exist (user ran scripts/build-npm.mjs first).
//   2. For the CURRENT host platform only (darwin-arm64, linux-x64, etc.):
//        - `npm pack` the matching @mooncodemaster/aios-<host> tarball
//        - `npm pack` the @mooncodemaster/aios main tarball
//        - install both into an isolated prefix (mktemp dir)
//        - run `aios --version` and verify it prints something semver-ish
//   3. Separately verify the unsupported-platform error path:
//        - invoke bin/aios.js directly with a spoofed process.platform via
//          a small Node harness and expect a clear, non-zero exit.
//
// The goal is not to reproduce the CI release; it is to catch mis-wired
// optionalDependencies, bad bin paths, or a broken launcher on THIS machine
// before a tag is pushed.
'use strict';

import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import process from 'node:process';

const repoRoot = resolve(fileURLToPath(import.meta.url), '..', '..');
const stageDir = join(repoRoot, 'dist', 'npm');

function log(...args) {
  console.error('[smoke-npm]', ...args);
}

function die(msg, code = 1) {
  console.error(`[smoke-npm] error: ${msg}`);
  process.exit(code);
}

function hostPackage() {
  const map = {
    'darwin-arm64': 'aios-darwin-arm64',
    'darwin-x64': 'aios-darwin-x64',
    'linux-arm64': 'aios-linux-arm64',
    'linux-x64': 'aios-linux-x64',
    'win32-x64': 'aios-win32-x64',
  };
  const key = `${process.platform}-${process.arch}`;
  const pkg = map[key];
  if (!pkg) die(`host ${key} is not in the supported platform table`);
  return pkg;
}

function ensureStage() {
  if (!existsSync(stageDir)) {
    die(
      `dist/npm/ not found.\n` +
        `  run the staging step first:\n` +
        `    goreleaser build --snapshot --clean\n` +
        `    AIOS_VERSION=0.0.0-smoke node scripts/build-npm.mjs`,
    );
  }
}

function runNpmPack(pkgDir, outDir) {
  const r = spawnSync('npm', ['pack', '--pack-destination', outDir, pkgDir], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  if (r.status !== 0) die(`npm pack failed for ${pkgDir}`);
  const tarball = r.stdout.trim().split('\n').filter(Boolean).pop();
  return join(outDir, tarball);
}

function verifyLaunchedBinary(prefix) {
  // Resolve the aios bin inside our isolated prefix.
  const aiosBin = process.platform === 'win32'
    ? join(prefix, 'aios.cmd')
    : join(prefix, 'bin', 'aios');
  if (!existsSync(aiosBin)) {
    die(`aios launcher not found in isolated prefix: ${aiosBin}`);
  }
  let out;
  try {
    out = execFileSync(aiosBin, ['--version'], { encoding: 'utf8', stdio: 'pipe' });
  } catch (err) {
    die(
      `aios --version failed: exit ${err.status ?? '?'}\n` +
        `  stderr: ${(err.stderr ?? '').toString().trim()}`,
    );
  }
  log(`aios --version → ${out.trim()}`);
  if (!/aios\s+version/i.test(out) && !/\d+\.\d+\.\d+/.test(out)) {
    die(`aios --version output does not look like a version string: "${out.trim()}"`);
  }
}

function smokeInstall() {
  const pkg = hostPackage();
  const pkgMainDir = join(stageDir, 'aios');
  const pkgHostDir = join(stageDir, pkg);

  if (!existsSync(pkgMainDir)) die(`missing staged main package: ${pkgMainDir}`);
  if (!existsSync(pkgHostDir)) die(`missing staged host package: ${pkgHostDir}`);

  const tmp = mkdtempSync(join(tmpdir(), 'aios-smoke-'));
  const packDir = join(tmp, 'packs');
  const prefix = join(tmp, 'prefix');
  mkdirSync(packDir, { recursive: true });
  mkdirSync(prefix, { recursive: true });
  log(`tmp: ${tmp}`);

  log('npm pack (platform package)');
  const platTar = runNpmPack(pkgHostDir, packDir);
  log(`  → ${platTar}`);

  log('npm pack (main package)');
  const mainTar = runNpmPack(pkgMainDir, packDir);
  log(`  → ${mainTar}`);

  // Install platform first, main second, into an isolated prefix so we do not
  // pollute the user's global node_modules. `--install-strategy=hoisted`
  // mirrors what -g would do; `--prefix` scopes it.
  const env = { ...process.env, npm_config_prefix: prefix };
  log('npm install (isolated prefix)');
  const ir = spawnSync(
    'npm',
    [
      'install',
      '-g',
      '--install-strategy=hoisted',
      '--prefix',
      prefix,
      platTar,
      mainTar,
    ],
    { encoding: 'utf8', stdio: 'inherit', env },
  );
  if (ir.status !== 0) die('npm install failed');

  verifyLaunchedBinary(prefix);

  log('cleaning tmp');
  rmSync(tmp, { recursive: true, force: true });
}

function smokeUnsupportedPlatform() {
  // Exercise the launcher's error path by running it under a Node harness
  // that redefines process.platform / process.arch before the launcher starts.
  // This does not require the native binary or npm install.
  const launcher = join(repoRoot, 'npm', 'aios', 'bin', 'aios.js');
  if (!existsSync(launcher)) die(`launcher not found: ${launcher}`);

  const harness = `
    Object.defineProperty(process, 'platform', { value: 'aix' });
    Object.defineProperty(process, 'arch', { value: 'ppc64' });
    try { require(${JSON.stringify(launcher)}); } catch (e) {
      console.error('harness: launcher threw', e.message);
    }
  `;
  const harnessFile = join(tmpdir(), `aios-smoke-unsupported-${process.pid}.js`);
  writeFileSync(harnessFile, harness);
  const r = spawnSync(process.execPath, [harnessFile], { encoding: 'utf8' });
  rmSync(harnessFile, { force: true });

  if (r.status === 0) {
    die('launcher exited 0 under spoofed unsupported platform — expected non-zero');
  }
  const stderr = r.stderr || '';
  if (!/unsupported platform/i.test(stderr)) {
    die(
      `launcher error output does not mention "unsupported platform":\n` + stderr,
    );
  }
  log(`unsupported-platform path OK (exit ${r.status})`);
}

function main() {
  log('starting smoke test');
  ensureStage();
  smokeInstall();
  smokeUnsupportedPlatform();
  log('all smoke checks passed');
}

main();
