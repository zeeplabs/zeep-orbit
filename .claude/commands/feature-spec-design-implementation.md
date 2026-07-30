---
name: feature-spec-design-implementation
description: Workflow command scaffold for feature-spec-design-implementation in zeep-orbit.
allowed_tools: ["Bash", "Read", "Write", "Grep", "Glob"]
---

# /feature-spec-design-implementation

Use this workflow when working on **feature-spec-design-implementation** in `zeep-orbit`.

## Goal

Implements a new feature by creating design, spec, and tasks documents, updating the roadmap, and implementing backend and/or frontend code.

## Common Files

- `.specs/features/*/design.md`
- `.specs/features/*/spec.md`
- `.specs/features/*/tasks.md`
- `.specs/project/ROADMAP.md`
- `internal/*/*.go`
- `internal/*/*.ts`

## Suggested Sequence

1. Understand the current state and failure mode before editing.
2. Make the smallest coherent change that satisfies the workflow goal.
3. Run the most relevant verification for touched files.
4. Summarize what changed and what still needs review.

## Typical Commit Signals

- Create or update .specs/features/<feature>/design.md
- Create or update .specs/features/<feature>/spec.md
- Create or update .specs/features/<feature>/tasks.md
- Update .specs/project/ROADMAP.md
- Implement backend logic (e.g., internal/provisioner, internal/dashboard, internal/config, etc.)

## Notes

- Treat this as a scaffold, not a hard-coded script.
- Update the command if the workflow evolves materially.