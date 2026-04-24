# AIOS platform packages

This directory holds the package.json templates for the five platform-specific
npm packages that carry the native `aios` binary. They are published
automatically by the release workflow and are not meant to be installed
directly — install `@mooncodemaster/aios` instead, which selects the correct
platform package for you via npm's `os` / `cpu` resolution.

Each template is committed with `version: "0.0.0-dev"` so it is a valid,
lintable JSON file in the repo. `scripts/build-npm.mjs` substitutes the
actual release version at publish time.
