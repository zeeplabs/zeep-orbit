# v0.2.0 — Brand Config Persistence Fix, Logo Upload Removed

## Highlights

### 🐛 Company Name / Theme Resetting on Restart

Fixed a bug where the Company Name and Theme saved in the dashboard Settings page silently reverted to the defaults (`Zeep Tecnologia` / `azure`) after a process restart.

**Root cause:** the server seeded `zeep_system.brand_config` on every boot via `UpsertBrandConfig`, passing the env var (or hardcoded default) as the value. That upsert uses `COALESCE(NULLIF($1, ''), old_value)` so an empty string preserves the existing value — but the seed call never sends an empty string, so it always overwrote whatever had been saved through the Settings UI.

**Fix:** startup seeding now uses a dedicated `SeedBrandConfig`, which inserts the row only if it doesn't exist yet (`ON CONFLICT ((TRUE)) DO NOTHING`) and never touches an existing row. Saved settings now survive restarts.

### 🎨 Logo Upload Removed

Removed the ability to upload a custom login logo / app icon from Settings. The dashboard and login page now always use the default Zeep Orbit logo.

- `BrandSettingsPage`: "Logo" tab removed
- Backend: `POST /api/brand/logo/{type}` endpoint and its S3 upload path removed

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.2.0
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml

# Upgrade existing release:
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```
