## 🚀 Zeep Orbit v0.1.1

Bug fixes, dashboard improvements, auth providers, and open-source polish.

### ✨ Features

#### 🔐 Auth Providers
- **Dashboard Google OAuth** — configure via UI (Settings → Google OAuth), credentials encrypted with AES-256-GCM
- **Per-App Login Providers** — each app can enable email and/or Google OAuth via the dashboard, with Client ID/Secret and allowed domains
- **`GET /{app}/auth/providers`** — endpoint listing active providers for the app frontend
- **`/{app}/auth/google/login` + callback** — per-app Google OAuth, returns JWT same as email login

#### 🖥️ Dashboard
- **Change password** — any user can change their own password; superadmin can change any user's password
- **App User Management** — list, search, deactivate/reactivate accounts, reset sessions, provider count
- **Sidebar menu** renamed from "Appearance" to "Settings"
- **Cache cleared on login/logout** — previous user data no longer appears after switching accounts

#### 📦 Open Source
- **Docusaurus documentation** (`/docs`) with auto-deploy via GitHub Pages
- **SVG architecture diagram**
- **Playwright E2E tests** for the dashboard
- **README badges** (CI, License, Go, Docker, Release)
- `SECURITY.md`, `CODE_OF_CONDUCT.md`, `SUPPORT.md`
- **GitHub Discussions** enabled
- Go reference docs (package comments)
- `CHANGELOG.md` and `ROADMAP.md` updated

### 🐛 Bug Fixes
- **Login 500** — `google_id` NULL caused panic (COALESCE fix)
- **FK violation** on Data Browser create (owner_id removed)
- **React Query cache** not cleared when switching users
- **App Users 500** — `active`/`provider` columns missing in existing apps

### 🔒 Security
- **AES-256-GCM** for secrets at rest (OAuth client credentials)
- Encrypted database storage with conditional reveal

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.1
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
