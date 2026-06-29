## 🚀 Zeep Orbit v0.1.4

Navigation fixes, login/logout flow improvements, and CI/CD polish.

### 🐛 Bug Fixes

- **Login redirect** — awaits React Query refetch before navigating to `/apps`
- **Logout redirect** — navigates to `/login` after state is fully cleaned up
- **Root URL** — accessing `http://localhost:8080` now redirects to `/dashboard`
- **Onboarding password** — minimum length aligned to 8 characters

### 📦 CI/CD

- **Helm chart published via Docker workflow** — no more separate chart release. Chart version is derived from the git tag and published directly to `gh-pages` branch, keeping releases clean
- **Removed `chart-release.yml`** — chart is now built and published inside the same workflow as Docker image and GitHub Release
- **Release notes** — v0.1.1 and v0.1.2 release notes in English
- **`RELEASE.md`** — step-by-step guide for the release process

### 🏗️ Deployment

- **EasyPanel template** — `easypanel.json` for one-click deploy
- **Deployment docs** — step-by-step guide for EasyPanel, Docker, Helm, Docker Compose

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.3
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
