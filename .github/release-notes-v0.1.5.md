## 🚀 Zeep Orbit v0.1.5

Login/logout redirect fix.

### 🐛 Bug Fixes

- **Login redirect** — now awaits React Query refetch before navigating to `/apps`, preventing the dashboard from loading before user data is ready
- **Logout redirect** — navigation to `/login` now happens after state cleanup is complete

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.5
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
