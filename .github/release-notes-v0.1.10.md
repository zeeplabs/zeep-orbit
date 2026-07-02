# v0.1.10 — Global S3, Brand Logos, Helm Fixes, i18n

## Highlights

### 🌐 Global S3 Configuration (Superadmin)
Superadmin can now configure global S3 storage in Settings → Storage (S3):

- Set endpoint, region, access key, secret key, and bucket once
- When global S3 is active, admins creating apps only provide a folder name — the app name is used automatically as a subfolder within the global bucket
- Files are stored as `s3://global-bucket/{app-folder}/app_{appname}/{file-id}.ext`
- Automatically merges global credentials into app storage config

### 🖼️ Brand Logos
Superadmin can upload custom logos that replace Orbit defaults:

- **Login Logo** — shown on the login page
- **App Icon** — shown in the dashboard sidebar
- Logos are stored in the global S3 bucket at `brand/login-logo` and `brand/icon`
- Instant sidebar update after upload (query cache invalidation)
- Fallback to built-in SVGs when no logo is configured

### ⚙️ Settings Page Redesigned with Tabs
The settings page now uses tabbed navigation (routed via `?tab=`):

| Tab | Content |
|-----|---------|
| Branding | Company name, theme selector, preview |
| Logo | Upload login logo and sidebar icon |
| Database | Soft delete toggle |
| Auth Provider | Google OAuth configuration |
| Storage (S3) | Global S3 configuration |

### 🐛 Bug Fixes
- **Helm chart 404** — GitHub Pages was configured in Actions deploy mode, but the Helm chart was being pushed to the `gh-pages` branch (which was ignored). Now the chart is packaged and served from the same Pages artifact at `https://zeeplabs.github.io/zeep-orbit/helm/`
- **Deployment `--config` flag** — The Helm deployment template was passing `--config /app/apps.yaml` to `zeep serve`, but `serve` doesn't accept a `--config` flag (it loads everything from the database). Removed the invalid argument.
- **Race condition on startup** — `ProvisionZeepSystem` now wraps all DDL in a single transaction with `pg_advisory_xact_lock`, serializing provisioning across concurrent pods (`replicaCount > 1`)
- **Google OAuth redirect_uri** — BrandSettingsPage now shows the correct callback URL placeholder (`/dashboard/api/auth/google/callback`)
- **Logo upload overwriting** — Uploading a new logo no longer clears the other logo (reads current config first)
- **Soft delete overwritten** — Saving global S3 config no longer resets `soft_delete_enabled` to false

### 🆕 New Features
- **Google Setup page** — First-time Google login users are prompted to set a name and password as a backup in case Google is unavailable
- **`needs_setup` field on `/api/me`** — Frontend can detect if a Google user needs to complete their account
- **`storage_configured` field on `/api/config`** — Frontend knows if global S3 is active
- **Public brand config endpoint** — `GET /dashboard/api/brand/config` returns logo URLs (unauthenticated, for login page)
- **Sonner toasts** — Replaced native `alert()` with toast notifications throughout the dashboard
- **All hardcoded texts reviewed** — Every user-facing string now uses i18n keys (`settings.*`, `common.*`, `appForm.storageGlobalHint`)

### 🖥️ Dashboard
- Settings page organized into 5 tabs (Branding, Logo, Database, Auth Provider, Storage)
- App form storage section adapted: when global S3 is active, only folder name is needed (read-only, auto-filled with app name)
- `BrandSettingsPage` — new `GlobalStorageCard` and `BrandLogoCard` components
- `DashboardShell` — dynamic icon from S3 with instant cache invalidation
- `LoginPage` — dynamic logo from S3

### 🔧 Helm Chart
- `secrets.storage.*` — new optional storage config section in values.yaml (generates `STORAGE_ENDPOINT`, `STORAGE_BUCKET`, etc.)
- Fixed `args: ["serve"]` — removed invalid `--config` flag
- README and release notes updated with correct Helm repo URL

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.10
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml

# Upgrade existing release:
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```
