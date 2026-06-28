---
sidebar_position: 2
---

# Creating Apps

The dashboard allows you to create and manage apps without writing YAML.

## Creating an App

1. Click **Novo App**
2. Enter the app name (lowercase, letters, numbers, underscores)
3. Configure **Login Providers** — toggle email/password and/or Google OAuth
4. Add **Tables** — define columns with names and types
5. Click **Create**

The app is provisioned instantly: schema created in PostgreSQL, tables created, REST API available.

## Editing an App

- Click the pencil icon on any app card
- Modify tables and columns
- Changes are applied idempotently

## Deleting an App

- Hover the app card and click the trash icon
- Confirm deletion
- Note: this only removes the app record from `zeep_system`, not the PostgreSQL schema
