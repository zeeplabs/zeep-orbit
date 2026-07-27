# v0.4.0 — Changelog, Brand Repositioning & Deploy Improvements

## Highlights

### ✨ Changelog Page

A new `/changelog` page in the dashboard shows the full release history — all 12 releases from v0.1.0 to v0.3.0, organized by version with categorized sections (Features, Improvements, Fixes, Security, Breaking Changes).

- **Embedded JSON** — the changelog is a static `changelog.json` file in the repository, compiled into the Go binary via `//go:embed`. No per-instance database, no external dependencies.
- **To add a new entry**: edit `internal/dashboard/changelog.json`, add the release to the `entries` array (newest first), commit, and release. Users see it automatically on upgrade.
- Timeline design with version badges, dates, colored section labels, and "Load more" pagination.
- Sidebar link with Megaphone icon positioned above the user section. Also available on mobile bottom bar.
- README section explaining the changelog approach.

### 🌐 Custom Domains for Frontend Apps

Frontend apps can now have custom domains configured directly from the dashboard.

- **Domain modal** — shows live preview of the full domain (subdomain + base domain) and DNS CNAME instructions pointing to the Render service URL.
- **Base domain** — configure in Integrations → Deploy. Each app gets a subdomain that combines with the base domain.
- **Inline domain editing** on the app card — click to open the domain modal.
- Backend integration with Render's custom domain API (`POST /v1/services/{id}/custom-domains`).

### ✨ Brand Repositioning

The platform identity shifted from "Database-first backend platform" to **"The complete platform for tech teams."**

- Login page title and subtitle updated across pt-BR and English.
- Onboarding page copy updated.
- Dashboard subtitle reflects the full scope (backend + frontend + deploy).
- README rewritten for the full platform — backend apps, frontend deploy, SDKs, and changelog, with an updated roadmap (M3-M7 marked done, M8 added).
- New `README.pt-BR.md` — GitHub auto-detects user locale and shows the Portuguese version.

### 🎨 UI/UX Improvements

- **Card redesign** — both backend and frontend app cards restructured with header/content/footer layout. Status rows organized per category. Creation date moved to the footer.
- **Loading states and toast notifications** added to every async operation — no more silent failures.
- **Unified delete confirmation dialog** — glass-morphism design used consistently across all delete actions.
- **Global `cursor-pointer`** on all button elements via the shadcn/ui Button base class.
- **Sync modal** now shows a description of what the deploy key is for and a "View usage instructions" button, instead of raw key reveal.
- **Deploy retry button** on frontend app cards when deploy fails.

### 🐛 Fixes

- **Login page** — subtitle now properly rendered instead of duplicating the title. Both title and subtitle centered.
- **Render API** — custom domain field corrected to `name` instead of `domain`.
- **Render API** — PATCH `/v1/services/{id}` used to assign `projectId` after creation.
- **Render API** — owner object properly unwrapped from `GET /v1/owners` response.
- **Render API** — request body logged on create service error for debugging.
- **Frontend** — broken JSX in AppsPage causing build failure fixed.
- **Domains** — subdomain and base domain whitespace trimmed before constructing the full domain URL.

### 📦 Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.4.0
```

### 📋 Helm

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml

# Upgrade existing release:
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```
