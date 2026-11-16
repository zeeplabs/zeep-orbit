# zeep-orbit

## Vision

Self-hosted platform for building apps with AI the right way: AI builds the frontend, wired to a real backend generated from the database schema — or the same backend alone, for frontends that already exist. One shared platform for multiple internal or SaaS applications, eliminating the need to create, deploy, and maintain individual backends for each app.

## Problem

AI tools (Claude Code, Lovable, Cursor, v0) generate complete frontends in minutes. Every frontend needs a backend. Today the options are:

1. **Build a backend per app** — unsustainable at scale, weeks of work, inconsistent
2. **Use Supabase / external BaaS** — data leaves company infrastructure, no governance, cost at scale
3. **Do nothing** — frontends never ship or ship without persistence

zeep-orbit eliminates all three problems: one platform, N apps, shared PostgreSQL, auto REST APIs, data inside your own infrastructure.

## Core Principle

Schema-in → REST-out. Define your tables in YAML, get REST APIs. No backend code.

## Positioning

> "Vibe coding done right: AI-built apps on a real backend."

Not described as a BaaS anymore. A trustworthy, self-hosted platform for AI-built apps, in two modes: a full app (AI-built frontend + schema-generated backend) or the backend alone for an existing frontend. Previous positioning (until 2026-11-13): "One backend for all your apps. Deploy once, connect many."

## Target Users

- **Companies with multiple internal tools** built by technical or non-technical teams using AI
- **Small product studios** building multiple SaaS products who don't want to maintain N individual backends
- **Engineering teams** who want governance over AI-generated applications

## Non-Goals (V1)

- Competing with Supabase as a cloud BaaS
- Replacing proper backends for complex domain logic
- Realtime / subscriptions
- GraphQL
- Storage / file uploads
- Workflow engine

## Success Criteria (MVP)

- One `apps.yaml` defines N apps with schemas
- `zeep apply` provisions schemas on PostgreSQL and starts serving REST APIs
- Any frontend can connect via `Authorization: Bearer <jwt>` and consume the APIs
- Deployable via `docker compose up` in under 5 minutes
- First external user (non-Zeep) deploys successfully
