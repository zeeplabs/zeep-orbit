```markdown
# zeep-orbit Development Patterns

> Auto-generated skill from repository analysis

## Overview

This skill teaches the core development patterns, coding conventions, and collaborative workflows used in the `zeep-orbit` repository. The project is primarily written in Go, with a TypeScript/React frontend, and is focused on backend provisioning, dashboard UI, and extensible schema management. It emphasizes clear documentation, internationalization, robust CI/CD, and structured release processes.

## Coding Conventions

**File Naming**
- Use `camelCase` for file names.
  - Example: `tableCard.go`, `userConfig.tsx`

**Import Style**
- Use import aliases for clarity.
  - Example (Go):
    ```go
    import cfg "zeep-orbit/internal/config"
    import dash "zeep-orbit/internal/dashboard"
    ```
  - Example (TypeScript):
    ```typescript
    import * as api from './lib/api'
    ```

**Export Style**
- Use named exports for functions, types, and components.
  - Example (Go):
    ```go
    func NewProvisioner() *Provisioner { ... }
    ```
  - Example (TypeScript):
    ```typescript
    export function fetchData() { ... }
    export const TableCard = () => { ... }
    ```

**Commit Patterns**
- Prefix commits with `feat`, `refactor`, or `release` as appropriate.
  - Example: `feat: add support for foreign keys in table schema`
  - Example: `refactor: extract dashboard logic into separate package`
  - Example: `release: bump version to v1.2.0`

## Workflows

### Feature Spec, Design & Implementation
**Trigger:** When adding a significant new feature or capability  
**Command:** `/new-feature`

1. Create or update `.specs/features/<feature>/design.md` with design details.
2. Create or update `.specs/features/<feature>/spec.md` with technical specifications.
3. Create or update `.specs/features/<feature>/tasks.md` to track implementation tasks.
4. Update `.specs/project/ROADMAP.md` to reflect the new feature.
5. Implement backend logic in appropriate Go packages (e.g., `internal/provisioner`, `internal/dashboard`, `internal/config`).
6. Implement or update frontend components if needed (e.g., `internal/dashboard/ui/src/components/FeatureCard.tsx`).
7. Write or update tests for the new logic.

**Example:**
```go
// internal/provisioner/feature.go
func EnableNewFeature(cfg *Config) error {
    // Implementation
}
```

---

### i18n & Localization Update
**Trigger:** When adding new UI text, supporting new languages, or improving localization  
**Command:** `/update-i18n`

1. Update or add translation keys in `internal/dashboard/ui/src/locales/en.json` and other locale files.
2. Refactor UI components/pages to use translation hooks (`useTranslation`).
3. Update or add translated text in UI components/pages.
4. Optionally, update README or documentation to reflect i18n changes.

**Example:**
```tsx
// internal/dashboard/ui/src/components/Header.tsx
import { useTranslation } from 'react-i18next';
const { t } = useTranslation();
return <h1>{t('dashboard.title')}</h1>;
```

---

### CI/CD Workflow Update
**Trigger:** When improving, refactoring, or adding CI/CD automation  
**Command:** `/update-ci`

1. Edit or add `.github/workflows/*.yml` files.
2. Refactor jobs into reusable workflows if needed.
3. Update release process documentation or scripts.
4. Test and verify workflow status checks.

**Example:**
```yaml
# .github/workflows/ci.yml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: 1.20
      - run: go test ./...
```

---

### Documentation Expansion & Localization
**Trigger:** When improving docs or adding new language support  
**Command:** `/update-docs`

1. Add or update `README.md` and its translations in `i18n/README.*.md`.
2. Update or add documentation files for features, contributing, or configuration.
3. Update or add docs in `docs/docs/*`.
4. Update `CHANGELOG.md` or `RELEASE.md` if relevant.

---

### Table Schema Feature Expansion
**Trigger:** When adding or changing table schema features (e.g., foreign keys, indexes, defaults)  
**Command:** `/expand-schema`

1. Update backend schema/config logic (`internal/config`, `internal/provisioner`, `internal/dashboard`).
2. Update or add tests for schema logic.
3. Update frontend components to support new schema features (e.g., `internal/dashboard/ui/src/components/TableCard.tsx`, `api.ts`).
4. Update or add translation keys for new UI elements.
5. Update documentation/specs for the new schema feature.

**Example:**
```go
// internal/config/schema.go
type TableSchema struct {
    Name    string
    Columns []Column
    Indexes []Index // new feature
}
```
```tsx
// internal/dashboard/ui/src/components/TableCard.tsx
<TableCard showIndexes={true} />
```

---

### Release Version Bump
**Trigger:** When releasing a new version  
**Command:** `/release`

1. Update `CHANGELOG.md` with new release notes.
2. Update version in `charts/zeep-orbit/Chart.yaml` and `internal/dashboard/ui/package.json`.
3. Add or update `internal/dashboard/changelog.json`.
4. Add or update `.github/release-notes-<version>.md`.
5. Commit, push changes, create tag, and trigger CI/CD workflows.

---

## Testing Patterns

- Test files follow the `*.test.*` pattern (e.g., `user.test.go`, `tableCard.test.tsx`).
- Testing framework is not explicitly specified; use Go's built-in `testing` package for backend and standard testing libraries (e.g., Jest, React Testing Library) for frontend.
- Place tests alongside implementation or in dedicated `__tests__` folders.

**Example (Go):**
```go
// internal/config/schema.test.go
func TestTableSchemaDefaults(t *testing.T) {
    // Test logic here
}
```

**Example (TypeScript):**
```typescript
// internal/dashboard/ui/src/components/TableCard.test.tsx
test('renders table card with indexes', () => {
    // Test logic here
});
```

## Commands

| Command        | Purpose                                                      |
|----------------|--------------------------------------------------------------|
| /new-feature   | Start a new feature: design, spec, tasks, roadmap, code      |
| /update-i18n   | Add or update internationalization/localization support       |
| /update-ci     | Update or refactor CI/CD workflows                           |
| /update-docs   | Add or update documentation and translations                 |
| /expand-schema | Expand or modify table schema features                       |
| /release       | Perform a release: version bump, changelog, release notes    |
```
