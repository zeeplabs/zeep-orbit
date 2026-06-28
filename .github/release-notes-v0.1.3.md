## 🚀 Zeep Orbit v0.1.3

Navigation fixes and deployment improvements.

### 🐛 Bug Fixes

- **Login redirect** — page now navigates to `/apps` immediately after successful login instead of waiting for React Query refetch
- **Logout redirect** — page now navigates to `/login` immediately after logout
- **Root URL** — `http://localhost:8080` now redirects to `/dashboard`
- **Onboarding password** — minimum length validation aligned to 8 characters across frontend and backend

### 📖 Documentation

- Deployment guide in Portuguese at `/docs/deployment` covering EasyPanel, Docker, Helm, Docker Compose
- v0.1.1 release notes translated to English

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
