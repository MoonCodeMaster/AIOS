---
name: demo
goal: Build a CLI that reverses argv.
non_goals:
  - "Handle Unicode normalization"
constraints:
  - "stdlib only"
acceptance_bar:
  - "aios/staging builds"
  - "go test ./... green"
---

# Project: demo

## Architecture sketch
Single main.go.
