# Release Process

Step-by-step to create a new Zeep Orbit release.

## 1. Update version files

### charts/zeep-orbit/Chart.yaml

```yaml
version: 0.1.3       # bump this
appVersion: "0.1.3"  # keep in sync
```

## 2. Update CHANGELOG.md

Move unreleased changes to a new version section at the top of `CHANGELOG.md`:

```markdown
## [0.1.3] — 2026-06-28

### Added
- ...

### Fixed
- ...

## [0.1.2] — 2026-06-28
...
```

## 3. Commit and push

```bash
git add -A
git commit -m "chore: bump version to 0.1.3"
git push origin main
```

## 4. Create and push tag

```bash
git tag v0.1.3
git push origin v0.1.3
```

## 5. CI does the rest

Pushing the tag triggers:

| Workflow | What it does |
|----------|-------------|
| `docker-publish.yml` | Builds multi-arch Docker image → pushes to `ghcr.io/zeeplabs/zeep-orbit:v0.1.3` |
| Same workflow | Creates GitHub Release with auto-generated release notes |
| `chart-release.yml` | Packages Helm chart → publishes to `gh-pages` branch |

## 6. Verify

- [ ] Docker image: `docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.3`
- [ ] GitHub Release: https://github.com/zeeplabs/zeep-orbit/releases
- [ ] Helm chart: `helm repo update zeeplabs && helm search repo zeeplabs/zeep-orbit --versions`

## Checklist

- [ ] `Chart.yaml` version bumped
- [ ] `Chart.yaml` appVersion bumped
- [ ] `CHANGELOG.md` updated
- [ ] All changes committed
- [ ] Tag pushed to GitHub
- [ ] CI workflows passed
- [ ] Docker pull works
- [ ] Helm install works
