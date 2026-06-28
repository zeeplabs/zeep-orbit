## 🚀 Zeep Orbit v0.1.2

Deployment improvements, onboarding fix, and documentation updates.

### ✨ Features

- **EasyPanel template** — one-click deploy via `easypanel.json`
- **Deployment docs page** — step-by-step guide for EasyPanel, Docker, Helm, Docker Compose
- **Helm chart aligned** — version `0.1.1` synced with the app release

### 🐛 Bug Fixes

- **Onboarding password** — minimum length validation fixed from 12 to 8 characters (aligned with backend)
- **Helm chart release** — created `gh-pages` branch for Helm repository publishing

### 📦 CI/CD

- **Docker publish** — workflow now triggers only on `v*` tags, not on push to main
- **Automatic release** — GitHub Release created automatically when pushing a tag
- **Chart release** — added `skip_existing` to prevent version conflicts

### 📖 Documentation

- EasyPanel deployment guide (App → Docker Image)
- Note about `host.docker.internal` vs `localhost` in Docker run command

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.2
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
