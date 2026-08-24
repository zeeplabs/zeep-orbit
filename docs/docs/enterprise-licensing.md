---
title: Enterprise Licensing
---
Zeep Orbit uses a dual-license model.

- **Core** — everything in this repository except the enterprise directories
below — is MIT licensed and free to use, self-host, and modify without
restriction.
- **Enterprise** — code under `internal/enterprise/` and its frontend mirror
`internal/dashboard/ui/src/enterprise/` — is source-available: you can read,
study, and modify it freely, and run it in non-production environments
without a license key. Running it in production requires an active
Enterprise License Key.

## Why

The core product stays open and self-hostable indefinitely. Zeep Tecnologia
funds ongoing development of the enterprise code through a subscription that
unlocks specific enterprise features in production, without limiting the
number of users, apps, or servers you run.

## What you can do without a license key

- Read, study, fork, and modify enterprise-directory code.
- Run it in development, CI, staging, or any other non-production
environment.
- Contribute changes back to the official repository.

## What requires an Enterprise License Key

- Running enterprise-directory code (or a modification of it) in a
production environment.

## What an Enterprise License Key does not restrict

- Number of users, seats, applications, projects, servers, or environments.
- Where you deploy (any country, subject to applicable law and sanctions).
- Using Zeep Orbit, including enterprise features, as internal infrastructure
for your own products — a SaaS product you build on top of Zeep Orbit does
not itself become subject to enterprise restrictions.

## What it does restrict

A standard Enterprise License Key does not authorize reselling, hosting, or
otherwise offering Zeep Orbit to third parties as a competing product or
service (for example: managed Zeep Orbit hosting, white-label, OEM, or
letting third parties run their own independent Orbit tenants under your
key). That requires a separate Reseller License Key under its own commercial
agreement.

## Commercial terms

License term, pricing, billing, automatic renewal, and the grace period
after a failed payment are governed by the
[Zeep Orbit Enterprise Commercial Subscription Terms](https://github.com/zeeplabs/zeep-orbit/blob/main/COMMERCIAL_TERMS.md),
not by the source license itself. The current model is an annual
subscription via Stripe, with a seven-day grace period after expiration or a
failed renewal before enterprise features are suspended. Suspension never
affects the MIT-licensed core, and never deletes your data.

## Full legal text

- [`LICENSE`](https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSE) — MIT
License for the core, plus the dual-license pointer.
- [`LICENSING.md`](https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSING.md)
— licensing model overview.
- [`internal/enterprise/LICENSE`](https://github.com/zeeplabs/zeep-orbit/blob/main/internal/enterprise/LICENSE)
— controlling (English) Enterprise Source License.
- `internal/enterprise/LICENSE.pt-BR.md` — Portuguese translation, for
convenience only.
- [`COMMERCIAL_TERMS.md`](https://github.com/zeeplabs/zeep-orbit/blob/main/COMMERCIAL_TERMS.md)
— controlling (English) subscription terms, with a Portuguese translation at
`COMMERCIAL_TERMS.pt-BR.md`.

