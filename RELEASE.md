# Release Process

Step-by-step to create a new Zeep Orbit release.

## 0. Open a release branch and PR into `main`

Day-to-day work happens on `develop`. Releases are cut from `main`, but never by pushing straight to it — go through a release branch + PR so CI runs and the version-bump diff gets reviewed before it lands.

```bash
git checkout develop
git pull origin develop
git checkout -b release-v0.4.1
git push origin release-v0.4.1
```

Open a PR `release-v0.4.1` → `main`. Do steps 1-4 below as commits on this branch (either locally before pushing, or as follow-up commits on the open PR). Once CI is green and the PR is reviewed, merge it using **"Rebase and merge"** — not "Create a merge commit" — to keep `main`'s history linear (it has no merge commits today).

After the PR merges, `main` has everything needed for the release. Continue from step 5 (tag).

## 1. Update version files

### charts/zeep-orbit/Chart.yaml

```yaml
version: 0.4.1      # bump this
appVersion: "0.4.1" # keep in sync with the release version
```

### internal/dashboard/ui/package.json

```json
{
  "version": "0.4.1"
}
```

This is the version shown in the dashboard UI (login page footer, sidebar) and is what the sidebar's update-available banner compares against the latest GitHub release tag — if it's not bumped, the banner keeps nagging users who are already on the new version.

## 2. Update CHANGELOG.md

Move unreleased changes to a new version section at the top of `CHANGELOG.md`:

```markdown
## [0.4.1] — 2026-07-30

### Added
- ...

### Fixed
- ...

## [0.4.0] — 2026-07-26
...
```

## 3. Add an entry to the in-app changelog

Edit `internal/dashboard/changelog.json` and add the new release to the `entries` array (newest first). This is what renders on the dashboard's `/changelog` page — it's embedded into the Go binary at compile time (`//go:embed`), so it must be updated in the same commit as the code that ships, not as an afterthought.

Every `title`/`summary`/section `items[].description` field is **bilingual** — `{"en": "...", "pt-BR": "..."}`, not a plain string and not an i18n key. `ChangelogPage.tsx`'s `localize()` helper picks the field matching `i18n.language` at render time, falling back to `en`. A plain string here silently renders as an empty title/summary in both languages (it happened once — don't repeat it).

```json
{
  "version": "v0.4.1",
  "release_date": "2026-07-30",
  "title": {"en": "...", "pt-BR": "..."},
  "summary": {"en": "...", "pt-BR": "..."},
  "sections": [
    {
      "type": "features",
      "items": [
        {"description": {"en": "...", "pt-BR": "..."}}
      ]
    }
  ]
}
```

`sections[].type` is one of `features` / `improvements` / `fixes` / `security` / `breaking` — each renders with its own color pill on the changelog page.

## 4. Write release notes

Create `.github/release-notes-v0.4.1.md` summarizing the release for humans (features, fixes, breaking changes, upgrade instructions). This is separate from the changelog files above — it's what actually becomes the **GitHub Release body** (see step 7): the `docker-publish.yml` release job reads `.github/release-notes-<tag>.md` and uses it verbatim (plus a Docker/Helm snippet appended automatically). If the file is missing for a given tag, CI falls back to GitHub's auto-generated notes instead — so skipping this step doesn't fail the release, it just publishes generic auto-generated notes.

## 5. Commit, push, and merge the release PR

```bash
git add -A
git commit -m "release: bump to v0.4.1"
git push origin release-v0.4.1
```

Merge the PR into `main` once CI is green (rebase and merge — see step 0).

> **Note:** Landing on `main` triggers `docs.yml`, which packages the Helm chart and publishes it to `https://zeeplabs.github.io/zeep-orbit/helm/`. The chart version comes from `Chart.yaml`.

## 6. Create and push tag

```bash
git tag v0.4.1
git push origin v0.4.1
```

## 7. CI does the rest

Pushing the tag triggers `docker-publish.yml`:

| Step | What it does |
|------|-------------|
| Test | Runs Go + frontend test suites |
| Build | Multi-arch Docker image, pushed to GHCR |
| Release | Creates the GitHub Release (tag `v0.4.1`), packages the Helm chart `.tgz` and attaches it |

The Helm chart repository at `https://zeeplabs.github.io/zeep-orbit/helm/` is updated separately by `docs.yml` when `Chart.yaml`'s version is bumped and pushed to `main` (step 5/6 above) — it does not wait for the tag.

## 8. Publish SDK Clients

Publish updated client packages after each release (only if the SDKs actually changed — not every release touches them):

### TypeScript (`@zeeptech/orbit-client`)

```bash
# Update version in clients/typescript/package.json
npm version patch  # or minor / major

# Build + publish
cd clients/typescript
npm run build
npm publish --access public
```

Required: npm token with 2FA bypass enabled at https://www.npmjs.com/settings/zeeptech/tokens

### Go (`github.com/zeeplabs/orbit-go`)

```bash
# Tag the Go module
cd clients/go
git tag clients/go/v0.1.0
git push origin clients/go/v0.1.0
```

Go modules are published by tag — no build step needed.

### Python (`zeeplabs-orbit-client`)

```bash
cd clients/python
python3 -m pip install --upgrade build twine
python3 -m build
python3 -m twine upload dist/*
```

### Rust (`orbit-client`)

```bash
cd clients/rust
cargo login        # one-time: set crates.io token
cargo publish      # reads version from Cargo.toml
```

### Java (`com.zeeplabs:orbit-client`)

```bash
cd clients/java
# Update version in pom.xml
mvn deploy         # requires Maven Central / Sonatype credentials
```

### PHP (`zeeplabs/orbit-client`)

```bash
cd clients/php
# Update version in composer.json
# Publish to Packagist via GitHub webhook or manual push
```

## 9. Verify

- [ ] Docker image: `docker pull ghcr.io/zeeplabs/zeep-orbit:v0.4.1`
- [ ] GitHub Release: https://github.com/zeeplabs/zeep-orbit/releases
- [ ] Helm chart: `helm repo update zeeplabs && helm search repo zeeplabs/zeep-orbit --versions`
- [ ] Dashboard `/changelog` page shows the new entry
- [ ] Dashboard sidebar update banner no longer shows (fresh deploy on the new version)
- [ ] npm: `npm view @zeeptech/orbit-client versions`
- [ ] Go: `go list -m github.com/zeeplabs/orbit-go@latest`
- [ ] PyPI: `pip install zeeplabs-orbit-client==0.1.0`
- [ ] crates.io: `cargo search orbit-client`

## Checklist

- [ ] Release branch `release-vX.Y.Z` created from `develop`
- [ ] `charts/zeep-orbit/Chart.yaml` version bumped (`version` + `appVersion`)
- [ ] `internal/dashboard/ui/package.json` version bumped
- [ ] `CHANGELOG.md` updated
- [ ] `internal/dashboard/changelog.json` entry added
- [ ] `.github/release-notes-vX.Y.Z.md` written
- [ ] PR opened, CI green, reviewed
- [ ] PR merged into `main` via **rebase and merge** (no merge commit)
- [ ] Tag pushed to GitHub (`git push origin v0.4.1`)
- [ ] CI workflows passed
- [ ] Docker pull works
- [ ] Helm install works (`helm repo update && helm install zeeplabs/zeep-orbit`)
- [ ] SDK clients published, if changed (TS / Go / Python / Rust / Java / PHP)
