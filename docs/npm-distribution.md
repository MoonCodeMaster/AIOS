# npm distribution — user guide

AIOS ships on npm as `@mooncodemaster/aios`. This page explains how the packages fit
together and how to troubleshoot install problems.

## What gets installed

```
@mooncodemaster/aios                    ← you install this one
├── bin/aios.js                  ← tiny Node launcher (this is all JS)
└── optionalDependencies:
    ├── @mooncodemaster/aios-darwin-arm64     ┐
    ├── @mooncodemaster/aios-darwin-x64       │  npm installs EXACTLY ONE of these,
    ├── @mooncodemaster/aios-linux-arm64      ├── selected by your platform/arch
    ├── @mooncodemaster/aios-linux-x64        │  (npm's `os` / `cpu` filter).
    └── @mooncodemaster/aios-win32-x64        ┘
```

Each platform package contains a single precompiled Go binary under `bin/`.
When you run `aios`, the launcher in `@mooncodemaster/aios` resolves the installed
sibling and execs its binary — no postinstall scripts, no network download,
no code generation, no runtime glue.

## Supported platforms

| `process.platform` | `process.arch` | Package                        |
|:-------------------|:---------------|:-------------------------------|
| `darwin`           | `arm64`        | `@mooncodemaster/aios-darwin-arm64`   |
| `darwin`           | `x64`          | `@mooncodemaster/aios-darwin-x64`     |
| `linux`            | `arm64`        | `@mooncodemaster/aios-linux-arm64`    |
| `linux`            | `x64`          | `@mooncodemaster/aios-linux-x64`      |
| `win32`            | `x64`          | `@mooncodemaster/aios-win32-x64`      |

If your platform isn't listed, open an issue — we add new platforms by adding
one row to `.goreleaser.yaml`, one directory under `npm/platforms/`, and one
entry in the launcher's `SUPPORTED` table.

## Troubleshooting

### `aios: the platform package "…" is not installed`

This means npm skipped your platform package. Causes:

1. **`--no-optional` / `--omit=optional`.** Optional deps are not optional for
   AIOS — the native binary lives in one of them. Re-install with:
   ```bash
   npm install -g @mooncodemaster/aios --include=optional
   ```

2. **Corporate registry mirror that doesn't mirror optionalDependencies.**
   Some Artifactory / Nexus / Verdaccio configurations cache only declared
   direct deps. Ask your registry admin to mirror the scope `@mooncodemaster/*`, or
   install the platform package explicitly:
   ```bash
   npm install -g @mooncodemaster/aios-linux-x64 @mooncodemaster/aios
   ```

3. **`npm install` ran on a different platform** (e.g. building a container
   on darwin-arm64 to run on linux-x64). Always run the install inside the
   final target environment, or set `--os`/`--cpu` to force npm to fetch a
   different platform:
   ```bash
   npm install --os=linux --cpu=x64 -g @mooncodemaster/aios
   ```

### `aios: unsupported platform <platform>-<arch>`

Your platform isn't in the supported list. For now install from source:

```bash
go install github.com/MoonCodeMaster/AIOS/cmd/aios@latest
```

Then open an issue naming the platform so we can add a package.

### `aios: failed to execute native binary: EACCES`

The binary lost its `+x` bit, usually by being copied through a tool that
strips file modes. Reinstall cleanly:

```bash
npm uninstall -g @mooncodemaster/aios
npm install   -g @mooncodemaster/aios
```

### `aios: failed to execute native binary: not found`

Some Node package managers (pnpm with strict isolation, yarn PnP) handle
platform-specific optionalDependencies differently. Try npm first:

```bash
npm install -g @mooncodemaster/aios
```

If you must use pnpm or yarn, set the relevant "include optional" flag for
your manager — the underlying mechanism is the same.

## Verifying an install

```bash
aios --version
aios init --help
```

Both should produce output. If `aios --version` prints nothing and the shell
reports a non-zero exit, run the launcher with Node verbosity:

```bash
NODE_DEBUG=child_process aios --version
```

and include the output in any issue you file.
