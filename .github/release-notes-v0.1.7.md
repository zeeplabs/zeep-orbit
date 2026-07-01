# v0.1.7 — SDKs Page, Todo Example App, Roadmap

## Highlights

### 🧭 SDKs Page in Dashboard
New `/sdks` route with installation snippets and copy-to-clipboard for all 6 official SDKs (TypeScript, Go, Python, Rust, Java, PHP). Accessible from the sidebar between Data Browser and Logs.

### 📘 Todo Example App
A complete React + Vite example at `examples/todo-app/` demonstrating real-world SDK usage:

- **Connect** — point to any Orbit instance and app name
- **Auth** — register and sign in via `client.auth`
- **Full CRUD** — create, list, inline edit, toggle completion, and delete todos via `client.table("todos")`
- No backend code — Orbit auto-generates the REST API from the table config

Run it:
```bash
cd examples/todo-app
npm install && npm run dev
```

### 🗺️ Roadmap in README
Public milestones M1–M6 with status indicators and deferred/backlog items.

### 🖥️ Dashboard
- New "SDKs" navigation entry in sidebar
- SDK icon on app cards

### 🐛 Bug Fixes
- Fix build artifacts in tsconfig tsbuildinfo (dashboard UI)

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.7
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```
