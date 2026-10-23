---
name: web-design-guidelines
description: Review UI code for Web Interface Guidelines compliance. Use when asked to "review my UI", "check accessibility", "audit design", "review UX", or "check my site against best practices".
metadata:
  author: vercel
  version: "1.0.0"
  argument-hint: <file-or-pattern>
---

# Web Interface Guidelines

Review files for compliance with Web Interface Guidelines.

> **Zeep Orbit local patch:** upstream fetched the rules from the `main` branch of `vercel-labs/web-interface-guidelines` on every use, so the rules changed without a PR and became agent instructions straight from the network. The rules are now vendored in `command.md` (same folder), pinned to commit `434b7f91364665f2f733b310ec54809bf8f37937` (2026-10-05), sha256 `d246b026f4f29b5823a9cc857f9edf3d2507002e055e32040cadeaf3b38e0234`. Do not fetch from the network. To update, see "Agent skills" in `AGENTS.md`.

## How It Works

1. Read `command.md` in this folder (the pinned guidelines)
2. Read the specified files (or prompt user for files/pattern)
3. Check against all rules in `command.md`
4. Output findings in the terse `file:line` format that `command.md` defines

## Usage

When a user provides a file or pattern argument:
1. Read `command.md` in this folder
2. Read the specified files
3. Apply all rules from `command.md`
4. Output findings using the format specified in `command.md`

If no files specified, ask the user which files to review.

## Zeep Orbit notes

- The "Content & Copy" rules (Title Case/Chicago style, active voice, numerals) are written for English. The dashboard is bilingual (`en` + `pt-BR` via `react-i18next`), so apply them to `en.json` only and skip them for `pt-BR.json`.
- Strings must go through `t()`; flag hardcoded user-facing strings in components (see `AGENTS.md` section 5).
