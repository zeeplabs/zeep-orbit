## Highlights

- **Responsive dashboard.** The layout now adapts to phone, tablet, and ultra-wide monitors: a 5-slot bottom bar with a "More" drawer below 768px, a 72px icon-only rail with tooltips between 768–1023px, and the existing full sidebar at 1024px and up. Main content caps at 1920px and centers on very wide screens instead of stretching indefinitely.
- **MCP client instructions.** The MCP server now sends `instructions` on `initialize`, so any MCP client can drive Orbit correctly without a separately-installed skill — tool categories, `enum`/partial-update/RLS-default gotchas, and the English-only error string convention.
- **`orbit-internals` skill** for agents working on this codebase itself, covering schema-per-app provisioning, RLS modes, and the shared REST/MCP operation pattern.
- **E2E CI now runs for real.** The Playwright suite executes across chromium/mobile/tablet/ultrawide on every PR and `develop` push. A login rate-limiter conflict that silently short-circuited the previous run was fixed at the root (shared session via `global-setup.ts` + `storageState`), and several legacy specs that had drifted from the current UI were repaired.

## Fixes

- Settings fields overflowing below 420px viewports (Brand Settings, GitHub Integration).
- Data Browser's two-pane layout collapsing too late on tablet widths.
- Sidebar/tablet-rail nav items missing active-state styling (a Radix `Tooltip.Trigger asChild` + `NavLink` style-function interaction).
- Sidebar section separator indexing, duplicated nav tooltips, and `SidebarFooter` overflow on the 72px tablet rail.
- `e2e` CI job that never actually ran every project (an early `set -e` abort masked `mobile`/`tablet`/`ultrawide`).
- `app-members.spec.ts` asserting a stale "Viewer" default role that no longer matched the component.

## Upgrade notes

No breaking changes, no migrations required. Standard upgrade via Helm chart or Docker image bump.

## Links

- Full changelog: [CHANGELOG.md](https://github.com/zeeplabs/zeep-orbit/blob/main/CHANGELOG.md)
