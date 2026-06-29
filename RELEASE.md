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
| `docker-publish.yml` | Test → Build multi-arch Docker image → Push to GHCR |
| Same workflow | Create GitHub Release with auto-generated notes |
| Same workflow | Package Helm chart → Update `gh-pages` branch (no separate release) |

The Helm chart version is automatically set from the git tag (e.g. `v0.1.3` → chart `0.1.3`).

## 6. Publish SDK Clients

Publish updated client packages after each release:

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

## 7. Verify

- [ ] Docker image: `docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.3`
- [ ] GitHub Release: https://github.com/zeeplabs/zeep-orbit/releases
- [ ] Helm chart: `helm repo update zeeplabs && helm search repo zeeplabs/zeep-orbit --versions`
- [ ] npm: `npm view @zeeptech/orbit-client versions`
- [ ] Go: `go list -m github.com/zeeplabs/orbit-go@latest`
- [ ] PyPI: `pip install zeeplabs-orbit-client==0.1.0`
- [ ] crates.io: `cargo search orbit-client`

## Checklist

- [ ] `CHANGELOG.md` updated
- [ ] All changes committed
- [ ] Tag pushed to GitHub
- [ ] CI workflows passed
- [ ] Docker pull works
- [ ] Helm install works
- [ ] SDK clients published (TS / Go / Python / Rust / Java / PHP)
