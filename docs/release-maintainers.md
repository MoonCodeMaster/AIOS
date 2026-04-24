# Release maintainers' runbook

This document is for the small set of humans with `npm publish` rights on the
`@mooncodemaster` scope. It covers:

1. One-time setup (NPM_TOKEN, scope permissions, CI secrets)
2. Cutting a release tag
3. Verifying the released packages
4. Rotating NPM_TOKEN
5. Migrating to npm Trusted Publishing (OIDC) when you're ready

## One-time setup

### 1. Create the `@mooncodemaster` scope on npm

If it doesn't already exist:

```bash
npm login
npm org create mooncodemaster   # pick the free tier; any plan works for public scope
```

Confirm you're an owner of the scope:

```bash
npm owner ls @mooncodemaster/aios    # 404 until first publish; that's fine
```

### 2. Generate an NPM_TOKEN for CI

Automation tokens live under [npmjs.com → Access Tokens](https://www.npmjs.com/settings/~/tokens).

- Type: **Granular access token** (not legacy)
- Packages: **scope** → `@mooncodemaster`
- Permissions: **Read and write**
- Expiry: **90 days** (mark a calendar reminder to rotate — see below)
- CIDR restriction: leave open unless you have a known-stable CI egress IP

Copy the token, paste it into **GitHub → repo Settings → Secrets and variables
→ Actions → Environment `npm-publish` → New secret `NPM_TOKEN`**. Using a
GitHub *environment* rather than a repo-level secret gives you:

- Required reviewers before a publish can run (optional but recommended)
- Clean audit trail of who approved which release
- Ability to add separate tokens for staging vs. production later

### 3. Protect the release workflow

Edit `.github/workflows/release.yml` — it already references the `npm-publish`
environment. In the repo settings, attach one or more required reviewers to
that environment so a rogue tag push cannot publish without approval.

## Cutting a release

Every published version is a single git tag. That is the only gesture you
need to make.

```bash
# bump version in CHANGELOG.md if you maintain one, commit, then:
git tag v0.1.0
git push origin v0.1.0
```

What happens:

1. `release.yml` triggers on the `v*` tag push.
2. Job `test` runs `go vet`, `go test`, and `node scripts/validate-npm.mjs`.
3. Job `goreleaser` builds binaries for all 5 platforms, creates the GitHub
   Release, and uploads `dist/npm/*` as an artifact for the next job.
4. Job `publish-npm`:
   - downloads the artifact
   - publishes each `@mooncodemaster/aios-<platform>` package
   - waits 10s for registry propagation
   - publishes `@mooncodemaster/aios`
   - runs `npm view @mooncodemaster/aios@<version>` to confirm

Total end-to-end time on cold cache: ~4–6 minutes.

**Dry-run the release without publishing:**

```bash
gh workflow run release.yml -f dry_run=true
```

The goreleaser job still runs (useful to validate binary builds); publish-npm
is skipped.

## Verifying a release

After the workflow reports green:

```bash
# Confirm the main package version and its optionalDependencies.
npm view @mooncodemaster/aios@<version>

# Confirm each platform package exists at the same version.
for p in darwin-arm64 darwin-x64 linux-arm64 linux-x64 win32-x64; do
  npm view "@mooncodemaster/aios-$p" version
done

# Smoke test from an empty machine / container.
docker run --rm -it node:20 bash -lc '
  npm install -g @mooncodemaster/aios
  aios --version
'
```

If any platform package is missing a version that matches the main, the most
likely cause is a mid-publish registry hiccup. Re-running the `publish-npm`
job from the Actions UI is idempotent — `npm publish` of an already-published
tarball is a no-op on the registry side (returns a 403 / "cannot publish over
previously published version", which the script treats as success if the
`npm view` confirmation step passes afterwards).

## Rotating NPM_TOKEN

Do this at least every 90 days, and immediately if:

- A maintainer with token access leaves the project
- A token fingerprint appears in any CI log, pastebin, or crash dump
- `npm audit` on the scope reports suspicious activity

Procedure:

1. Generate a new granular token in the npm UI (same scope, same
   permissions, fresh 90-day expiry).
2. In GitHub → Environment `npm-publish` → update `NPM_TOKEN` secret.
3. Revoke the old token in the npm UI.
4. Push a **no-op** tag (e.g. `v0.1.0-rotation-check`) and run the release
   workflow with `dry_run=true` to confirm CI can still publish.
5. Delete the rotation-check tag.

Never commit a token to the repo. If a token is accidentally committed:
revoke it immediately, then `git filter-repo` or `BFG` the history on a
rewind-eligible branch and force-push. Tokens in commit history are
considered compromised from the moment of commit, not from the moment of
discovery.

## Future: npm Trusted Publishing (OIDC)

npm supports OIDC-based trusted publishing, which removes NPM_TOKEN
entirely. When you're ready to switch:

1. In the npm UI, open each of `@mooncodemaster/aios` and the five platform
   packages.
2. Enable **Trusted Publishing**, specifying:
   - Repository: `MoonCodeMaster/AIOS`
   - Workflow: `release.yml`
   - Environment (optional): `npm-publish`
3. Remove `NPM_TOKEN` from the GitHub environment.
4. In `release.yml` the current `NODE_AUTH_TOKEN`/`NPM_TOKEN` envs become
   unnecessary; `npm publish --provenance` is all you need — it already
   picks up the GitHub OIDC token via `id-token: write`.

At that point this document can drop the "Rotating NPM_TOKEN" section; until
then, rotate on schedule.
