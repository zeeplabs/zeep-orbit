---
sidebar_position: 2
---

# Quick Start

## Docker Compose

```bash
git clone https://github.com/zeeplabs/zeep-orbit
cd zeep-orbit
cp .env.example .env
docker compose up -d
```

Visit **http://localhost:8080/dashboard** to access the management dashboard.

## First-time setup

1. Open the dashboard URL
2. You'll see the onboarding wizard
3. Enter the bootstrap secret (from `DASHBOARD_BOOTSTRAP_SECRET`)
4. Create your superadmin account
5. Start creating apps

## Binary

```bash
go install github.com/zeeplabs/zeep-orbit/cmd/zeep@latest
zeep serve --config ./apps.yaml
```

## Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit \
  --set secrets.databaseUrl=postgres://... \
  --set 'secrets.apps.myapp.jwtSecret=...'
```
