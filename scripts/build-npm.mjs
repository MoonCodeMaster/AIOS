#!/usr/bin/env node
// build-npm.mjs — stage publishable npm packages under dist/npm/*.
//
// Inputs:
//   - npm/aios/           main package source (package.json, bin/aios.js, README)
//   - npm/platforms/*/    per-platform package.json templates
//   - dist/               GoReleaser output containing the built native binaries
//   - AIOS_VERSION env    the release version (no leading v); fallback: git describe
//
// Output:
//   - dist/npm/aios/                    ready-to-publish main package
//   - dist/npm/aios-darwin-arm64/       ready-to-publish platform package
//   - dist/npm/aios-darwin-x64/         "
//   - dist/npm/aios-linux-arm64/        "
//   - dist/npm/aios-linux-x64/          "
//   - dist/npm/aios-win32-x64/          "
//
// This script deliberately has ZERO runtime dependencies beyond Node core, so
// it runs before any `npm install` — including on a clean CI runner.
'use strict';

import { execSync } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync, chmodSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import process from 'node:process';

const repoRoot = resolve(fileURLToPath(import.meta.url), '..', '..');
const npmSrc = join(repoRoot, 'npm');
const distDir = join(repoRoot, 'dist');
const stageDir = join(distDir, 'npm');
const licenseSrc = join(repoRoot, 'LICENSE');

// Platform → GoReleaser binary location. GoReleaser writes native binaries to
// dist/<build-id>_<os>_<arch>[_<variant>]/<binary>. The current .goreleaser
// build id is "aios". amd64 builds on linux and windows carry the GOAMD64
// suffix "_v1" by default; darwin does not. We probe the expected paths in
// order and use the first match, so tweaks to the goreleaser config do not
// immediately break the packager.
const PLATFORMS = [
  {
    pkg: 'aios-darwin-arm64',
    candidates: ['aios_darwin_arm64/aios', 'aios_darwin_arm64_v8.0/aios'],
    binName: 'aios',
    chmod: 0o755,
  },
  {
    pkg: 'aios-darwin-x64',
    candidates: ['aios_darwin_amd64/aios', 'aios_darwin_amd64_v1/aios'],
    binName: 'aios',
    chmod: 0o755,
  },
  {
    pkg: 'aios-linux-arm64',
    candidates: ['aios_linux_arm64/aios', 'aios_linux_arm64_v8.0/aios'],
    binName: 'aios',
    chmod: 0o755,
  },
  {
    pkg: 'aios-linux-x64',
    candidates: ['aios_linux_amd64/aios', 'aios_linux_amd64_v1/aios'],
    binName: 'aios',
    chmod: 0o755,
  },
  {
    pkg: 'aios-win32-x64',
    candidates: ['aios_windows_amd64/aios.exe', 'aios_windows_amd64_v1/aios.exe'],
    binName: 'aios.exe',
    // Windows does not use the chmod bit; still set it for consistency when
    // staged on a POSIX CI runner before tarball creation.
    chmod: 0o755,
  },
];

function log(...args) {
  console.error('[build-npm]', ...args);
}

function die(msg, code = 1) {
  console.error(`[build-npm] error: ${msg}`);
  process.exit(code);
}

function resolveVersion() {
  let v = process.env.AIOS_VERSION;
  if (!v) {
    try {
      v = execSync('git describe --tags --always --dirty', { cwd: repoRoot })
        .toString()
        .trim();
    } catch {
      v = 'dev';
    }
  }
  // Strip a single leading "v" so v0.1.0 → 0.1.0 (semver format npm expects).
  if (v.startsWith('v')) v = v.slice(1);
  // A "dev" or git-describe fallback ("<tag>-<n>-g<sha>") is allowed for
  // local smoke testing; the release workflow always sets AIOS_VERSION to a
  // clean semver so published packages carry only semver versions.
  if (!/^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/.test(v)) {
    log(`warning: version "${v}" is not strict semver; npm publish will reject it`);
  }
  return v;
}

function findBinary(candidates) {
  for (const rel of candidates) {
    const p = join(distDir, rel);
    if (existsSync(p)) return p;
  }
  return null;
}

function cleanStage() {
  if (existsSync(stageDir)) {
    log(`cleaning ${stageDir}`);
    rmSync(stageDir, { recursive: true, force: true });
  }
  mkdirSync(stageDir, { recursive: true });
}

// applyVersion mutates a package.json object: sets .version, and if present
// rewrites optionalDependencies so every sibling is pinned to the same
// version as the main package. Exact-match pins are required because otherwise
// a user on darwin-arm64 could end up resolving an older linux-x64 sibling
// and getting cross-version drift.
function applyVersion(pkg, version) {
  pkg.version = version;
  if (pkg.optionalDependencies) {
    for (const name of Object.keys(pkg.optionalDependencies)) {
      pkg.optionalDependencies[name] = version;
    }
  }
  return pkg;
}

function stageMainPackage(version) {
  const src = join(npmSrc, 'aios');
  const dst = join(stageDir, 'aios');
  log(`staging main package → ${dst}`);
  cpSync(src, dst, { recursive: true });
  // Copy the repo LICENSE in (single source of truth — not duplicated in git).
  cpSync(licenseSrc, join(dst, 'LICENSE'));
  // Rewrite version in-place.
  const pj = JSON.parse(readFileSync(join(dst, 'package.json'), 'utf8'));
  applyVersion(pj, version);
  writeFileSync(join(dst, 'package.json'), JSON.stringify(pj, null, 2) + '\n');
  // bin shim must be executable for `npm install` to symlink it correctly.
  chmodSync(join(dst, 'bin', 'aios.js'), 0o755);
}

function stagePlatformPackage(entry, version) {
  const srcDir = join(npmSrc, 'platforms', entry.pkg.replace(/^aios-/, ''));
  const dstDir = join(stageDir, entry.pkg);
  log(`staging platform package ${entry.pkg} → ${dstDir}`);

  // Skeleton: copy the template package.json (rewrite below), synthesize
  // a short README, copy LICENSE, and place the binary under bin/.
  mkdirSync(join(dstDir, 'bin'), { recursive: true });

  const pjPath = join(srcDir, 'package.json');
  if (!existsSync(pjPath)) die(`missing template: ${pjPath}`);
  const pj = JSON.parse(readFileSync(pjPath, 'utf8'));
  applyVersion(pj, version);
  writeFileSync(join(dstDir, 'package.json'), JSON.stringify(pj, null, 2) + '\n');

  cpSync(licenseSrc, join(dstDir, 'LICENSE'));
  writeFileSync(
    join(dstDir, 'README.md'),
    `# ${pj.name}\n\n` +
      `Platform-specific native binary for [AIOS](https://github.com/MoonCodeMaster/AIOS).\n\n` +
      `**Do not install this package directly.** Install the main package, which\n` +
      `selects the correct sibling for your platform automatically:\n\n` +
      '```bash\nnpm install -g @mooncodemaster/aios\n```\n\n' +
      `Target: \`${pj.os.join(',')}\` / \`${pj.cpu.join(',')}\`. Version: \`${pj.version}\`.\n`,
  );

  const binSrc = findBinary(entry.candidates);
  if (!binSrc) {
    die(
      `native binary not found for ${entry.pkg}.\n` +
        `  looked in: ${entry.candidates.map((c) => join(distDir, c)).join(', ')}\n` +
        `  did goreleaser run? try: goreleaser build --snapshot --clean`,
    );
  }
  const binDst = join(dstDir, 'bin', entry.binName);
  cpSync(binSrc, binDst);
  chmodSync(binDst, entry.chmod);
  const size = statSync(binDst).size;
  log(`  binary: ${binSrc} (${size} bytes) → bin/${entry.binName}`);
}

function main() {
  const args = new Set(process.argv.slice(2));
  const version = resolveVersion();
  log(`version: ${version}`);

  if (!existsSync(distDir) && !args.has('--allow-missing-dist')) {
    die(
      `dist/ not found. run goreleaser first:\n` +
        `    goreleaser build --snapshot --clean\n` +
        `  or pass --allow-missing-dist to stage only the main package (no binaries).`,
    );
  }

  cleanStage();
  stageMainPackage(version);

  if (args.has('--allow-missing-dist')) {
    log('skipping platform packages (--allow-missing-dist set)');
  } else {
    for (const entry of PLATFORMS) {
      stagePlatformPackage(entry, version);
    }
  }

  // Emit a manifest the release workflow consumes to publish in the right
  // order: platform packages first, then the main package.
  const manifest = {
    version,
    publishOrder: [
      ...PLATFORMS.map((p) => ({ name: `@mooncodemaster/${p.pkg}`, dir: `dist/npm/${p.pkg}` })),
      { name: '@mooncodemaster/aios', dir: 'dist/npm/aios' },
    ],
  };
  writeFileSync(join(stageDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  log(`wrote ${join(stageDir, 'manifest.json')}`);
  log('done.');
}

main();
