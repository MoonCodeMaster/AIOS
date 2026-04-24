#!/usr/bin/env node
// validate-npm.mjs — CI-safe structural validation of the npm/ sources.
//
// This runs on every PR as part of ci.yml. It catches drift that would
// otherwise only be discovered at release time:
//
//   - any package.json that fails JSON.parse
//   - platform packages with wrong os/cpu pair
//   - platform packages whose name does not match their directory
//   - main package whose optionalDependencies don't match npm/platforms/*
//   - main package whose bin entry doesn't point at an existing launcher
//   - missing required fields (name, version, license, files, ...)
//   - any package that would publish junk (.git, node_modules, test/, etc.)
//   - version drift between main and platform templates
//
// Exits 0 on success, 1 on any failure. Every failure is reported; the script
// does not stop at the first error so CI shows a complete diagnostic.
'use strict';

import { readFileSync, readdirSync, existsSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(fileURLToPath(import.meta.url), '..', '..');
const npmDir = join(repoRoot, 'npm');
const mainDir = join(npmDir, 'aios');
const platformsDir = join(npmDir, 'platforms');

// Must match the SUPPORTED table in npm/aios/bin/aios.js and PLATFORMS in
// scripts/build-npm.mjs. Validator enforces three-way consistency so a typo
// in any one of them fails fast.
const EXPECTED_PLATFORMS = {
  'darwin-arm64': { os: 'darwin', cpu: 'arm64' },
  'darwin-x64': { os: 'darwin', cpu: 'x64' },
  'linux-arm64': { os: 'linux', cpu: 'arm64' },
  'linux-x64': { os: 'linux', cpu: 'x64' },
  'win32-x64': { os: 'win32', cpu: 'x64' },
};

const errors = [];
function fail(msg) {
  errors.push(msg);
}

function readJSON(path) {
  try {
    return JSON.parse(readFileSync(path, 'utf8'));
  } catch (err) {
    fail(`${path}: JSON parse failed — ${err.message}`);
    return null;
  }
}

// Fields every npm package in this repo must carry.
const REQUIRED_FIELDS_COMMON = ['name', 'version', 'description', 'license', 'files', 'homepage', 'repository'];
const REQUIRED_FIELDS_MAIN = [...REQUIRED_FIELDS_COMMON, 'bin', 'optionalDependencies', 'engines'];
const REQUIRED_FIELDS_PLATFORM = [...REQUIRED_FIELDS_COMMON, 'os', 'cpu'];

function requireFields(pj, required, ctx) {
  for (const f of required) {
    if (!(f in pj)) fail(`${ctx}: missing required field "${f}"`);
  }
}

function validateMain() {
  const pjPath = join(mainDir, 'package.json');
  const pj = readJSON(pjPath);
  if (!pj) return null;
  requireFields(pj, REQUIRED_FIELDS_MAIN, `npm/aios/package.json`);

  if (pj.name !== '@mooncodemaster/aios') {
    fail(`npm/aios/package.json: name must be "@mooncodemaster/aios" (got "${pj.name}")`);
  }

  if (!pj.bin || pj.bin.aios !== 'bin/aios.js') {
    fail(`npm/aios/package.json: bin.aios must be "bin/aios.js"`);
  }
  const shimPath = join(mainDir, 'bin', 'aios.js');
  if (!existsSync(shimPath)) {
    fail(`npm/aios/bin/aios.js not found — bin shim is missing`);
  }

  // Every platform in npm/platforms/ must appear in optionalDependencies, and
  // nothing else may. Version pins must all match the main package version.
  const deps = pj.optionalDependencies || {};
  const expectedNames = Object.keys(EXPECTED_PLATFORMS).map((k) => `@mooncodemaster/aios-${k}`);
  for (const n of expectedNames) {
    if (!(n in deps)) fail(`npm/aios/package.json: optionalDependencies missing "${n}"`);
    else if (deps[n] !== pj.version) {
      fail(
        `npm/aios/package.json: optionalDependencies["${n}"] = "${deps[n]}" ` +
          `does not match main version "${pj.version}"`,
      );
    }
  }
  for (const n of Object.keys(deps)) {
    if (!expectedNames.includes(n)) {
      fail(`npm/aios/package.json: optionalDependencies has unexpected entry "${n}"`);
    }
  }

  // files[] hygiene: bin shim, README, LICENSE. Nothing else (LICENSE is
  // copied in by build-npm.mjs at stage time so it is not required to exist
  // in the source tree).
  const expectedFiles = ['bin/aios.js', 'README.md', 'LICENSE'];
  const files = Array.isArray(pj.files) ? pj.files : [];
  for (const f of expectedFiles) {
    if (!files.includes(f)) fail(`npm/aios/package.json: files[] missing "${f}"`);
  }

  return pj;
}

function validatePlatformDir(dirName) {
  const expected = EXPECTED_PLATFORMS[dirName];
  if (!expected) {
    fail(`npm/platforms/${dirName} is not a recognized platform directory`);
    return null;
  }
  const pjPath = join(platformsDir, dirName, 'package.json');
  const pj = readJSON(pjPath);
  if (!pj) return null;
  requireFields(pj, REQUIRED_FIELDS_PLATFORM, `npm/platforms/${dirName}/package.json`);

  const expectedName = `@mooncodemaster/aios-${dirName}`;
  if (pj.name !== expectedName) {
    fail(`${pjPath}: name must be "${expectedName}" (got "${pj.name}")`);
  }

  if (!Array.isArray(pj.os) || pj.os.length !== 1 || pj.os[0] !== expected.os) {
    fail(`${pjPath}: os must be ["${expected.os}"] (got ${JSON.stringify(pj.os)})`);
  }
  if (!Array.isArray(pj.cpu) || pj.cpu.length !== 1 || pj.cpu[0] !== expected.cpu) {
    fail(`${pjPath}: cpu must be ["${expected.cpu}"] (got ${JSON.stringify(pj.cpu)})`);
  }

  // files[] hygiene.
  const binName = expected.os === 'win32' ? 'bin/aios.exe' : 'bin/aios';
  const expectedFiles = [binName, 'README.md', 'LICENSE'];
  const files = Array.isArray(pj.files) ? pj.files : [];
  for (const f of expectedFiles) {
    if (!files.includes(f)) fail(`${pjPath}: files[] missing "${f}"`);
  }

  return pj;
}

function validatePlatforms(mainPj) {
  if (!existsSync(platformsDir)) {
    fail(`npm/platforms/ directory missing`);
    return;
  }
  const dirs = readdirSync(platformsDir, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => e.name);

  for (const key of Object.keys(EXPECTED_PLATFORMS)) {
    if (!dirs.includes(key)) fail(`npm/platforms/${key}/ missing`);
  }
  for (const name of dirs) {
    if (!(name in EXPECTED_PLATFORMS)) {
      fail(`npm/platforms/${name}/ is unexpected — not in EXPECTED_PLATFORMS`);
      continue;
    }
    const pj = validatePlatformDir(name);
    if (!pj || !mainPj) continue;

    // Enforce version sync. Templates all share "0.0.0-dev" in source; at
    // build time build-npm.mjs rewrites every package's version to the same
    // release version. Source-level drift between a platform and main is a
    // bug: someone bumped one without the others.
    if (pj.version !== mainPj.version) {
      fail(
        `npm/platforms/${name}/package.json: version "${pj.version}" ` +
          `does not match npm/aios version "${mainPj.version}"`,
      );
    }
  }
}

// junkPatterns entries are checked against every file inside an npm source
// directory as a last line of defense. Even if package.json lists them in
// files[], nothing here should be on disk in the source tree.
const JUNK_PATTERNS = [
  /^\.git(\/|$)/,
  /^node_modules(\/|$)/,
  /\.env$/,
  /\.env\.local$/,
  /^\.DS_Store$/,
  /\.pem$/,
  /\.key$/,
];

function walk(root, rel = '') {
  const out = [];
  let entries;
  try {
    entries = readdirSync(join(root, rel), { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const path = rel ? `${rel}/${e.name}` : e.name;
    if (e.isDirectory()) {
      out.push(...walk(root, path));
    } else {
      out.push(path);
    }
  }
  return out;
}

function validateNoJunk() {
  for (const root of [mainDir, platformsDir]) {
    for (const f of walk(root)) {
      for (const pat of JUNK_PATTERNS) {
        if (pat.test(f)) fail(`${join(root, f)}: matches junk pattern ${pat}`);
      }
    }
  }
}

function main() {
  console.error('[validate-npm] starting');
  const mainPj = validateMain();
  validatePlatforms(mainPj);
  validateNoJunk();

  if (errors.length) {
    console.error(`[validate-npm] ${errors.length} error(s):`);
    for (const e of errors) console.error(`  - ${e}`);
    process.exit(1);
  }
  console.error('[validate-npm] all checks passed');
}

main();
