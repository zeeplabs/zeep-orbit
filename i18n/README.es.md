<div align="center">
  <img src="../docs/static/img/orbit-logo.png" alt="Zeep Orbit" width="200" />
  <p><strong>La plataforma completa para equipos de tecnología.</strong></p>

  <p>
    <a href="https://github.com/zeeplabs/zeep-orbit/actions"><img src="https://github.com/zeeplabs/zeep-orbit/actions/workflows/docker-publish.yml/badge.svg" alt="CI" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License" /></a>
    <a href="https://go.dev/doc/devel/release"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go" /></a>
    <a href="https://github.com/zeeplabs/zeep-orbit/releases"><img src="https://img.shields.io/github/v/release/zeeplabs/zeep-orbit" alt="Release" /></a>
  </p>

  <p>
    <a href="../README.md">🇺🇸 English</a> ·
    <a href="README.pt-BR.md">🇧🇷 Português (Brasil)</a> ·
    <a href="README.pt-PT.md">🇵🇹 Português (Portugal)</a> ·
    <a href="README.es.md">🇪🇸 Español</a>
  </p>
</div>

---

**Zeep Orbit** es una plataforma open-source y self-hosted que le da a tu equipo todo lo necesario para crear y publicar apps — APIs backend, despliegue de frontend, dominios personalizados y gestión de usuarios — todo desde un único dashboard. Sin servicios externos, sin lock-in. Tu infraestructura, tus datos.

<p align="center">
  <img src="../docs/static/img/diagram.svg" alt="Diagrama de Arquitectura" width="800" />
</p>

```bash
# Apps backend — define tablas, obtén APIs REST al instante
docker compose up -d
curl -H "Authorization: Bearer $TOKEN" localhost:8080/miapp/tareas
# → {"data":[],"count":0}

# Apps frontend — elige una plantilla, obtén un sitio en vivo con dominio personalizado
# Conecta GitHub, elige Vite + React, despliega en Render con un clic
```

---

## 📑 Índice

- [Funcionalidades](#-funcionalidades)
- [Inicio rápido](#-inicio-rápido)
  - [Docker Compose](#docker-compose)
  - [Kubernetes (Helm)](#kubernetes-helm)
  - [Binario](#binario)
- [Dashboard](#%EF%B8%8F-dashboard)
- [Apps Backend](#%EF%B8%8F-apps-backend)
- [Apps Frontend](#%EF%B8%8F-apps-frontend)
- [SDK Clients](#-sdk-clients)
- [CLI](#-cli)
- [Servidor MCP](#-servidor-mcp)
- [Observabilidad](#-observabilidad)
- [Despliegue](#-despliegue)
  - [Docker](#docker)
  - [Kubernetes (Helm)](#kubernetes-helm-1)
- [Configuración](#-configuración)
- [Changelog](#-changelog)
- [Roadmap](#%EF%B8%8F-roadmap)
- [Desarrollo](#%EF%B8%8F-desarrollo)
- [Contribuir](#-contribuir)
- [Licencia](#-licencia)

---

## ✨ Funcionalidades

### Apps Backend

| Funcionalidad          | Descripción                                              |
| ---------------------- | -------------------------------------------------------- |
| **Schema → REST**      | Define tablas en el dashboard → API CRUD instantánea     |
| **Relaciones e Índices** | Foreign keys (`references` + `on_delete`) e índices en el schema builder, validados y ordenados automáticamente |
| **Auth por Email**     | Registro e inicio de sesión con email/contraseña por app |
| **Google OAuth**       | Inicio de sesión con Google — dashboard y por app        |
| **Row-Level Security** | Filtra datos automáticamente por dueño (`rls: owner`/`rls: enabled`), con opción de "exigir RLS por defecto" en tablas nuevas; `rls: policy` elimina por completo el filtro automático de dueño, delegando la visibilidad y el permiso de escritura al 100% a las table policies nativas de Postgres — incluyendo policies que dan a un rol acceso a filas de otros usuarios |
| **Políticas de Fila para Usuario Final** | Reglas de acceso por fila, por tabla/acción, combinando el rol de negocio del usuario final con una condición sobre los datos de la propia fila, aplicadas por RLS nativo de Postgres (`CREATE POLICY`) — no un filtro en la capa Go |
| **Roles de Usuario Final Configurables** | Define tu propia lista de roles de negocio por app, usada por las políticas de fila y mostrada en la gestión de usuarios del app |
| **App Tokens**         | Gestión de JWT para apps sin auth por email (crear, revocar, renovar) |
| **Health por App**     | `GET /{app}/health` para monitoreo y readiness probes    |
| **Soft Delete**        | Toggle configurable de eliminación lógica (settings del dashboard) |
| **Retención y Purga**  | Job en background elimina definitivamente filas soft-deleted fuera de la ventana de retención (apagado por defecto, auditado) |
| **Timeout de Query**   | `statement_timeout` global en las queries de datos de las apps (por defecto 30s, `0` lo desactiva) |
| **Rate Limiting**      | Sliding window por app y por IP (RPM configurable)       |
| **Almacenamiento de archivos** | Almacenamiento S3-compatible por app (DO Spaces, AWS, MinIO) |

### Apps Frontend

| Funcionalidad            | Descripción                                              |
| ------------------------ | -------------------------------------------------------- |
| **Integración con GitHub** | Conecta una GitHub App, gestiona plantillas y deploy keys |
| **Sistema de plantillas** | Plantillas preconfiguradas (Vite + React + TypeScript)   |
| **Deploy con un clic**   | Crea el repo desde la plantilla, despliega en Render automáticamente |
| **Dominios personalizados** | Configura dominio propio + DNS CNAME para cada frontend |
| **Credenciales de sincronización** | Deploy keys por app para sincronizar local↔repo   |
| **Deploys recientes**    | Lista en vivo de los últimos deploys en Render de tus apps frontend |

### Plataforma

| Funcionalidad           | Descripción                                              |
| ----------------------- | -------------------------------------------------------- |
| **Dashboard Web**       | UI oscura premium para gestionar todo                    |
| **Data Browser**        | GUI para navegar, filtrar, editar, eliminar filas y exportar CSV (límite de filas configurable) |
| **Gestión de usuarios** | Administra usuarios del dashboard y usuarios de cada app |
| **Acceso por rol**      | 4 roles de plataforma (superadmin/admin/auditor/member) con matriz de permisos para UI y backend |
| **Roles por app**        | 3 roles por app (admin/editor/viewer) con UI de gestión de miembros; invariante ≥1 admin aplicado via transacción |
| **Audit Logs**          | Historial de acciones con filtros (quién, qué, cuándo, IP) |
| **CORS**                | Soporte cross-origin para SPAs y apps móviles            |
| **Documentación OpenAPI** | Swagger UI auto-generado por app                       |
| **White-label**         | Marca personalizada, temas, nombre de empresa            |
| **Métricas Prometheus** | `zeep_http_requests_total`, histogramas de latencia      |
| **Multi-app**           | Un servicio, N apps, schemas y JWT secrets aislados      |
| **CLI**                 | `zeep serve`, `zeep status`                              |
| **Kubernetes**          | Helm chart production-grade (HPA, PDB, ingress, IRSA)    |
| **SDK Clients**         | TypeScript, Go, Python, Rust, Java, PHP                  |
| **i18n**                | Dashboard en pt-BR / English, selector de idioma         |
| **Changelog**           | Historial de releases embebido en el dashboard           |
| **Notificaciones de actualización** | Aviso en el sidebar cuando hay un nuevo release en GitHub |
| **Servidor MCP**        | Servidor Model Context Protocol — crea e inspecciona apps, tablas, políticas de fila, miembros, tokens y webhooks desde Claude Code, Codex, Cursor, OpenCode (PAT) o Claude Desktop (OAuth 2.1) |

---

## 🚀 Inicio rápido

### Docker Compose

```yaml
services:
  zeep:
    image: ghcr.io/zeeplabs/zeep-orbit:latest
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://zeep:zeep@db:5432/zeep?sslmode=disable
      DASHBOARD_BOOTSTRAP_SECRET: change-me
    depends_on:
      db:
        condition: service_healthy

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: zeep
      POSTGRES_PASSWORD: zeep
      POSTGRES_DB: zeep
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zeep"]
      interval: 5s
      timeout: 5s
      retries: 5
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

```bash
docker compose up -d
```

Luego visita **http://localhost:8080/dashboard** para completar la configuración inicial.

> **Nota:** Si PostgreSQL corre en la máquina anfitriona (no en otro contenedor), usa `host.docker.internal` en lugar de `localhost` en `DATABASE_URL`.

### Binario

```bash
go install github.com/zeeplabs/zeep-orbit/cmd/zeep@latest
zeep serve
```

### Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit \
  --values values.yaml
```

→ Ver la sección [Kubernetes (Helm)](#kubernetes-helm-1) en **Despliegue** para la guía completa.

---

## 🖥️ Dashboard

El dashboard web está embebido en el binario y es accesible en `/dashboard`:

- **Apps** — crea apps backend (base de datos + API) o apps frontend (repo de GitHub + deploy)
- **Data Browser** — navega, filtra, ordena, edita en línea, elimina y exporta a CSV
- **Usuarios** — administra usuarios del dashboard (roles superadmin/admin/auditor/member)
- **Usuarios de App** — visualiza usuarios registrados en cada app, desactiva cuentas, reinicia sesiones
- **Miembros** — gestión de miembros por app (roles admin/editor/viewer); la pestaña "Miembros" en la página de detalles de la app permite al admin añadir, cambiar rol y eliminar miembros; el invariante ≥1 admin impide eliminar al último admin
- **Integraciones** — configuración de GitHub App, plantillas de deploy, proveedor de deploy Render
- **Logs** — log de requests en tiempo real con desglose de métricas
- **Auditoría** — historial de acciones con usuario, tipo de acción, recurso, IP y paginación
- **SDKs** — snippets de instalación para los 6 SDKs oficiales
- **Configuración** — marca white-label (temas, nombre de empresa), configuración de Google OAuth
- **Changelog** — historial de releases embebido en el binario, actualizado en cada release
- **i18n** — dashboard disponible en pt-BR e inglés, selector de idioma en el sidebar

---

## 🗄️ Apps Backend

Las apps backend te dan un schema de PostgreSQL + API REST instantánea a partir de una definición de tabla.

### Auth

Cada app soporta proveedores de login configurables:

| Proveedor | Endpoint                       | Descripción                     |
| --------- | ------------------------------ | -------------------------------- |
| Email     | `POST /{app}/auth/register`    | Registro con email + contraseña |
| Email     | `POST /{app}/auth/login`       | Login con email + contraseña    |
| Google    | `GET /{app}/auth/google/login` | Inicio de sesión con Google     |
| Todos     | `GET /{app}/auth/providers`    | Lista de proveedores habilitados |

Tras autenticarte, recibes un token JWT firmado con el secret de la app.

### API REST

| Método    | Ruta                  | Descripción                          |
| --------- | --------------------- | -------------------------------------- |
| GET       | `/{app}/{table}`      | Listar (paginado, filtrado, ordenado) |
| POST      | `/{app}/{table}`      | Crear                                  |
| GET       | `/{app}/{table}/{id}` | Obtener por ID                        |
| PUT/PATCH | `/{app}/{table}/{id}` | Actualizar (parcial)                  |
| DELETE    | `/{app}/{table}/{id}` | Eliminar (soft-delete si está activo) |
| GET       | `/{app}/health`       | Health check por app (sin auth)       |
| GET       | `/health`             | Health check global                   |
| GET       | `/metrics`            | Métricas Prometheus                   |
| GET       | `/docs/{app}`         | Swagger UI                            |
| GET       | `/{app}/auth/*`       | Endpoints de autenticación            |
| POST      | `/{app}/files`        | Subir archivo (multipart)             |
| GET       | `/{app}/files`        | Listar archivos                       |
| GET       | `/{app}/files/{id}`   | Obtener metadata del archivo          |
| GET       | `/{app}/files/{id}/download` | Descargar archivo (302 → URL firmada) |
| GET       | `/{app}/files/{id}/url` | Obtener URL firmada con TTL         |
| DELETE    | `/{app}/files/{id}`   | Eliminar archivo                      |

Parámetros de query para listar: `?limit=`, `?offset=`, `?field=eq.value`, `?order=field.asc`, `?deleted=true` (registros con soft-delete cuando está activo).

### Tipos de columna

`text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`

Opciones: `required` (NOT NULL), `unique`, `default` (expresión SQL).

Columnas auto-generadas: `id` (UUID), `created_at`, `updated_at`, `deleted_at` (nullable, usada cuando el soft delete está activo).

### Relaciones e índices

Una tabla puede declarar foreign keys (`references`: tabla/columna destino + `on_delete`) e índices. El schema se valida antes de ejecutar cualquier DDL (tabla/columna inexistente, `on_delete` inválido, nombres de índice duplicados, dependencias circulares de FK) y las tablas se crean en orden de dependencia. El aprovisionamiento de índices es idempotente — nada se elimina implícitamente — y borrar una tabla todavía referenciada por la foreign key de otra se rechaza con un error claro.

### App Tokens

Para apps sin auth por email/contraseña, puedes crear tokens de API con expiración configurable (7d, 30d, 365d, o nunca). Los tokens usan JWT con `jti` único — revocables individualmente, con un endpoint de renovación que extiende la expiración. La revocación se verifica en cada request vía una caché en memoria con invalidación inmediata al revocar.

---

## 🖥️ Apps Frontend

Las apps frontend te permiten desplegar sitios y apps web sin configuración:

1. **Conecta GitHub** — instala la GitHub App de Zeep Orbit en tu organización
2. **Añade una plantilla** — configura un repo starter (ej. Vite + React + TypeScript)
3. **Crea una app frontend** — elige la plantilla, define un subdominio
4. **Sincroniza** — recibe una deploy key para clonar el repo localmente y subir cambios
5. **Despliega** — deploy automático en Render con dominio personalizado configurado

### Proveedor de Deploy

- **Render** — configura API key, project ID, environment ID y dominio base. Cada app frontend se despliega como un static site de Render con configuración automática de dominio personalizado. Render asigna los servicios a un *Environment*, no a un Project: el environment se resuelve automáticamente cuando el project tiene solo uno, y debe indicarse explícitamente en caso contrario. El dashboard también muestra los deploys más recientes de tus apps frontend.

### Integración con GitHub

Zeep Orbit se conecta a GitHub mediante una **GitHub App** — nunca OAuth ni un personal access token, así que ninguna credencial personal queda almacenada. Cada instancia self-hosted crea y conecta su propia App a su propia org de GitHub (es un producto self-hosted: una instancia, una empresa, una App — no existe una App compartida/central para instalar).

**1. Crea la GitHub App** — `https://github.com/organizations/<tu-org>/settings/apps/new` (o en la configuración de desarrollador de tu cuenta personal, si no usás una org):

| Campo | Valor |
|---|---|
| GitHub App name | Cualquier nombre único (debe ser único en todo GitHub), ej: `acme-zeep-orbit` |
| Homepage URL | La URL de tu instancia, o este repo |
| Callback URL | Dejar vacío — no se usa, este flujo nunca hace OAuth de login de usuario |
| Setup URL | `https://<tu-instancia>/dashboard/api/github/install/callback` |
| Webhook → Active | Desmarcado — ningún evento de webhook se consume hoy |
| Repository permissions → Administration | Read and write |
| Where can this GitHub App be installed | Only on this account |

Generá una **private key** en la página de configuración de la App (descarga un archivo `.pem`) y anotá **App ID**, **App slug**, **Client ID** y **Client Secret**.

**2. Configurá en el dashboard** — andá a **Integraciones → Configuración** y pegá el App ID, App slug, Client ID, Client Secret, y el contenido completo del archivo `.pem` de la private key.

**3. Instalá** — hacé clic en **Instalar**, que corre el flujo nativo de instalación de GitHub. Elegí siempre **"Only select repositories"** y marcá los repos que esta instancia debe gestionar — la API crea repos nuevos y gestiona deploy keys solo dentro de ese alcance, nunca "All repositories".

**4. Agregá un repositorio template** — en la pestaña **Templates**, registrá un repo marcado como **Template repository** en GitHub (`Settings → Template repository` en el propio repo). Este es el que se clona cada vez que alguien crea una nueva app frontend.

Una vez conectado, las deploy keys se gestionan automáticamente por app frontend, y los repos se archivan (no se eliminan) cuando se borra una app frontend.

---

## 📦 SDK Clients

Clientes oficiales para todos los lenguajes principales. Misma API en todos:

```typescript
// TypeScript
import { OrbitClient } from '@zeeptech/orbit-client'
const orbit = new OrbitClient({ baseURL, app: 'myapp', jwt })
const rows = await orbit.table('invoices').findMany({ limit: 10 })
```

```go
// Go
import "github.com/zeeplabs/orbit-go"
client := orbit.New(orbit.ClientConfig{BaseURL, "myapp", jwt})
rows, err := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{Limit: 10})
```

```python
# Python
from zeeplabs_orbit_client import OrbitClient, ClientConfig
orbit = OrbitClient(ClientConfig(baseURL, "myapp", jwt))
rows = orbit.table("invoices").find_many(limit=10)
```

```rust
// Rust
use orbit_client::OrbitClient;
let orbit = OrbitClient::new(cfg);
let rows = orbit.table("invoices").find_many(Some(10), None, None, None).await?;
```

```java
// Java
OrbitClient orbit = new OrbitClient(new ClientConfig(baseURL, "myapp", jwt));
ListResponse resp = orbit.table("invoices").findMany(10, 0, null, null);
```

```php
// PHP
$orbit = new Zeeplabs\Orbit\OrbitClient($baseURL, 'myapp', $jwt);
$rows = $orbit->table('invoices')->findMany(limit: 10);
```

| Lenguaje | Paquete | Ruta |
|---|---|---|
| TypeScript | `@zeeptech/orbit-client` | `clients/typescript/` |
| Go | `github.com/zeeplabs/orbit-go` | `clients/go/` |
| Python | `zeeplabs-orbit-client` | `clients/python/` |
| Rust | `orbit-client` | `clients/rust/` |
| Java | `com.zeeplabs:orbit-client` | `clients/java/` |
| PHP | `zeeplabs/orbit-client` | `clients/php/` |

---

## 🔧 CLI

```
Comandos:
  serve    Aprovisiona el schema interno, inicia el servidor HTTP + Dashboard
  status   Verifica si el servidor está corriendo
```

```bash
zeep serve --port 8080
```

Las apps y tablas se crean y gestionan completamente a través del Dashboard (o, en el futuro, el servidor MCP) — no hay ningún archivo YAML que escribir ni paso de `apply`.

---

## 🔌 Servidor MCP

Zeep Orbit incluye un servidor [Model Context Protocol](https://modelcontextprotocol.io) para que los asistentes de código con IA creen e inspeccionen apps, tablas, políticas de fila, miembros, tokens y webhooks directamente — sin necesidad de usar el dashboard.

- **Endpoint:** `https://<host>/dashboard/mcp` — Streamable HTTP, stateless (seguro detrás de un load balancer no-sticky / múltiples réplicas)
- **Autenticación:** dos métodos, ambos resueltos contra el mismo store de Personal Access Token:
  - **Personal Access Token (PAT)** — genera uno en **Dashboard → MCP**, luego envíalo como bearer token. Esto es lo que usan Claude Code, Codex, Cursor y OpenCode.
  - **OAuth 2.1 + PKCE** — registro dinámico de cliente, flujo authorization code, rotación de refresh token, discovery en `/.well-known/oauth-authorization-server`. Esto es lo que usa Claude Desktop en su flujo interactivo de conexión.
- **Tools expuestas:** `orbit_list_apps`, `orbit_get_app_schema`, `orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_list_policy_templates`, `orbit_create_policy_from_template` — mismo camino de validación, aprovisionamiento y auditoría que la REST API y el dashboard, sin atajos.

### Configuración del cliente

Genera un PAT primero (**Dashboard → MCP**), expórtalo como variable de entorno y configura tu cliente:

**Claude Code** — `.mcp.json`:
```json
{
  "mcpServers": {
    "zeep-orbit": {
      "type": "http",
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      }
    }
  }
}
```

**Codex** — `~/.codex/config.toml`:
```toml
[mcp_servers.zeep-orbit]
url = "https://<host>/dashboard/mcp"
bearer_token_env_var = "ZEEP_ORBIT_PAT"
```

**Cursor** — `.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "zeep-orbit": {
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      }
    }
  }
}
```

**OpenCode** — `opencode.json`:
```json
{
  "mcp": {
    "zeep-orbit": {
      "type": "remote",
      "url": "https://<host>/dashboard/mcp",
      "headers": {
        "Authorization": "Bearer ${ZEEP_ORBIT_PAT}"
      },
      "enabled": true
    }
  }
}
```

> Trata el PAT como una contraseña — nunca lo subas al repositorio. Si tu cliente no soporta interpolación de variable `${VAR}` en su archivo de configuración, mantén ese archivo fuera del control de versiones.

---

## 📊 Observabilidad

- **Métricas Prometheus** en `/metrics`: número de requests, latencia, apps activas
- **Logging JSON estructurado** vía `zap` (define `LOG_LEVEL=debug`)
- **Logs del dashboard** con ring buffer en tiempo real, métricas y filtrado por app

---

## 🐳 Despliegue

### Docker

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest
docker run -e DATABASE_URL=... -p 8080:8080 ghcr.io/zeeplabs/zeep-orbit
```

### Kubernetes (Helm)

El Helm chart incluye: HPA, PDB, Ingress, ServiceMonitor, ServiceAccount listo para IRSA, topology spread y límites de recursos configurables.

> **Importante:** El comando `zeep serve` carga todo desde la base de datos. Las apps se crean y gestionan a través del Dashboard en `/dashboard`. Solo necesitas la base de datos.

#### Configuración mínima

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"
```

1. Instala con Helm → `helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml`
2. Accede a `https://tu-dominio/dashboard`
3. Usa el `dashboardBootstrapSecret` en el formulario de bootstrap
4. Crea usuarios admin y apps a través de la interfaz

Las apps creadas en el Dashboard persisten en la base de datos y se cargan en cada reinicio.

#### Ejemplo completo (dashboard + Google OAuth + storage)

```yaml
# values.yaml
secrets:
  databaseUrl: "postgres://user:pass@host:5432/zeep?sslmode=require"
  dashboardBootstrapSecret: "my-admin-secret"

  google:
    clientId: "123.apps.googleusercontent.com"
    clientSecret: "GOCSPX-xxxx"
    redirectUrl: "https://orbit.tusite.com/dashboard/api/auth/google/callback"
    allowedDomains: "tusite.com"

  storage:
    endpoint: "https://s3.amazonaws.com"
    bucket: "my-bucket"
    region: "us-east-1"
    accessKeyId: "AKIA..."
    secretAccessKey: "wJalrX..."

brand:
  theme: "azure"
  companyName: "Mi Empresa"
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml
```

#### Actualizando

```bash
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit -n <namespace> --reuse-values --atomic
```

`--reuse-values` mantiene los values ya configurados en el release (secrets, configuración) —
no hace falta tener un `values.yaml` local a mano; usa `--values values.yaml` en su lugar si vas
a cambiar alguna configuración. `-n <namespace>` debe coincidir con el namespace donde se instaló
el release (verifica con `helm list -A`). La flag `--atomic` revierte automáticamente si la
actualización falla.

El `image.tag` por defecto es `latest`, así que solo con `helm upgrade` no se recrean los pods si
la cadena del tag no cambió — Kubernetes solo hace rollout cuando cambia el pod spec. Fuerza el
pull de la nueva imagen con:

```bash
kubectl rollout restart deploy/zeep-orbit -n <namespace>
kubectl rollout status deploy/zeep-orbit -n <namespace>
```

En producción, fija `image.tag` o `image.digest` en tu `values.yaml` en vez de depender de
`latest` — así el `helm upgrade` solo ya dispara el rollout.

---

## 📋 Configuración

### Variables de entorno

| Variable                     | Requerida | Descripción                                          |
| ----------------------------- | --------- | ----------------------------------------------------- |
| `DATABASE_URL`                | Sí        | Cadena de conexión de PostgreSQL                      |
| `DASHBOARD_BOOTSTRAP_SECRET`  | Sí        | Secret para la configuración inicial del admin        |
| `GOOGLE_CLIENT_ID`            | No        | Client ID de Google OAuth (para login del dashboard)  |
| `GOOGLE_CLIENT_SECRET`        | No        | Client Secret de Google OAuth                         |
| `GOOGLE_REDIRECT_URL`         | No        | URL de redirección de Google OAuth                    |
| `GOOGLE_ALLOWED_DOMAINS`      | No        | Dominios de email permitidos, separados por coma      |
| `GOOGLE_OAUTH_ENCRYPTION_KEY` | No        | Cifra el client secret de Google OAuth en reposo (por defecto: `DASHBOARD_BOOTSTRAP_SECRET`) |
| `WEBHOOK_TOKEN_ENCRYPTION_KEY` | No       | Cifra los tokens de webhook entrante en reposo (por defecto: `DASHBOARD_BOOTSTRAP_SECRET`; separada a propósito, para rotar una sin invalidar la otra) |
| `BRAND_THEME`                 | No        | Tema por defecto (azure, emerald, ruby, amber, orange) |
| `BRAND_COMPANY_NAME`          | No        | Nombre de la empresa para white-label                 |
| `LOG_LEVEL`                   | No        | Define `debug` para output de desarrollo              |
| `DASHBOARD_LOG_BUFFER_SIZE`   | No        | Tamaño del ring buffer del visor de logs (default: 2000) |
| `ORBIT_PUBLIC_URL`            | No        | URL base visible externamente (ej: `https://orbit.example.com`) para el documento de metadatos OAuth 2.1 (`/.well-known/oauth-authorization-server`). Sin ella, la URL se deriva de los encabezados `Host`/`X-Forwarded-Proto` de la petición — configúrala cuando Orbit no esté detrás de un proxy que valide esos encabezados, para que un cliente MCP no pueda ser dirigido a un endpoint de token falsificado. |

---

## 📝 Changelog

Cada release de Zeep Orbit se publica con un changelog embebido — sin dependencias externas, sin base de datos por instancia. El [changelog](../internal/dashboard/changelog.json) es un archivo JSON estático en el repositorio, embebido en el binario en tiempo de compilación. Los usuarios ven las últimas novedades en el dashboard en `/changelog` automáticamente en cada actualización.

Para añadir una entrada nueva: edita `internal/dashboard/changelog.json`, añade tu release al array `entries` (más reciente primero), haz commit, y publica el release. Eso es todo.

---

## 🗺️ Roadmap

El detalle completo (checklists por milestone, specs vinculadas) vive en [`.specs/project/ROADMAP.md`](../.specs/project/ROADMAP.md) — esta tabla es un resumen, sincronizado con él.

| Milestone | Estado | Funcionalidades |
|---|---|---|
| **M1 — MVP Core** | ✅ Hecho | Schema → REST, CLI, Docker Compose |
| **M2 — Developer Experience** | ✅ Hecho | Dashboard, SDKs, relaciones e índices, migraciones, filtrado/ordenamiento |
| **M3 — Apps Frontend** | ✅ Hecho | Integración con GitHub, Plantillas, Deploy en Render, Dominios personalizados |
| **M4 — Gobernanza & Seguridad** | 🔵 En desarrollo | Audit Log, Soft Delete + retención/purga, SSO, Rate Limiting, [RBAC por app](../.specs/features/rbac-per-app/) (admin/editor/viewer), [roles globales del dashboard](../.specs/features/dashboard-global-roles/) (superadmin/admin/auditor/member) · planeado: [2FA](../.specs/features/two-factor-auth/), flujo de aprobación de cambios de schema |
| **M5 — Storage & Events** | 🔵 En desarrollo | Almacenamiento S3, [Webhooks Entrantes](../.specs/features/inbound-webhooks/) · planeado: webhooks salientes, event bus |
| **M6 — i18n** | ✅ Hecho | pt-BR / English, selector de idioma |
| **M7 — SDKs** | ✅ Hecho | Clientes TS, Go, Python, Rust, Java, PHP |
| **M8 — Platform Services** | 🔵 En desarrollo | planeado: [integración SMTP/email](../.specs/features/smtp-email-integration/) (invitaciones, recuperación de contraseña), [integraciones de observabilidad](../.specs/features/observability-integrations/) (OpenTelemetry, Datadog, New Relic) |
| **M9 — Enterprise Licensing** | 🔵 En desarrollo | planeado: [modelo de licenciamiento dual](../.specs/features/enterprise-licensing/) (núcleo MIT + funcionalidades enterprise bloqueadas, suscripción anual) |
| **M10 — Autorización de fila (usuario final)** | ✅ Hecho | [Políticas de fila por usuario final](../.specs/features/end-user-row-policies/) (claim de rol de negocio + RLS nativo de Postgres configurado por el admin) y [roles de usuario final configurables por app](../.specs/features/enduser-roles-config/) |

### Planeado — visible en el dashboard, todavía no funcional

Algunos de estos ítems ya aparecen en el dashboard como controles deshabilitados o badge "Pronto", para dejar el roadmap visible donde va a aterrizar. **No tienen backend hoy** y no hacen nada al hacer clic:

| Ítem | Dónde aparece |
|---|---|
| Autenticación de dos factores ([spec](../.specs/features/two-factor-auth/)) | Configuración → Proveedor de auth ("Exigir 2FA para todos los admins"), Usuarios del dashboard (acción "Resetear 2FA") |
| Aprobación de cambios de schema | Configuración → Base de datos ("Exigir aprobación de cambios de schema") |
| Licenciamiento enterprise ([spec](../.specs/features/enterprise-licensing/)) | Configuración → Licencia (preview solo de UI, pestaña bloqueada) |
| Code hosting: GitLab, Bitbucket | Integraciones → Configuración (selector de proveedor) |
| Proveedores de deploy: Cloudflare Pages, DigitalOcean, AWS, Azure, Google Cloud | Integraciones → Proveedores de deploy (selector de proveedor) |
| Proveedores de auth del dashboard: Microsoft Entra ID, Sign in with Apple, GitHub | Configuración → Proveedor de auth |
| Proveedores de storage: Azure Blob Storage, Google Cloud Storage | Configuración → Storage |
| Creación de app asistida por IA | Apps → "Crear con IA" |

### Diferido / Backlog

- Inicio de sesión con Apple (por app)
- Generador de código para el SDK de TypeScript (`@zeeptech/orbit-generate`)
- Snippets oficiales de prompt para Claude Code / Cursor / Lovable
- Auto-generación de GraphQL
- Subscripciones en tiempo real (WebSockets)
- Edge functions
- Soporte multi-región
- Marketplace de plantillas de apps
- RBAC con permisos granulares por acción (más allá de los niveles fijos admin/editor/viewer)
- SSO Microsoft Entra ID

---

## 🛠️ Desarrollo

```bash
git clone https://github.com/zeeplabs/zeep-orbit
make build        # compila el binario Go + la UI del dashboard
make test         # tests unitarios (no requiere base de datos)
make lint         # go vet
make run          # go run ./cmd/zeep
```

Los tests de integración requieren PostgreSQL:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...
```

### Estructura del proyecto

```
cmd/zeep/                  Entrypoint del CLI
internal/
  auth/                    Handlers de auth (registro, login, Google OAuth)
  config/                  Cargador de configuración YAML + validación
  crypto/                  Encriptación AES-256-GCM
  dashboard/               Backend del dashboard + UI React + changelog
    changelog.json         Historial de releases (embebido en el binario)
  db/                      Cliente pgxpool
  deploy/                  Interfaz de proveedor de deploy + implementación de Render
  docs/                    Generador de spec OpenAPI
  github/                  Cliente de GitHub App (repos, deploy keys, plantillas)
  provisioner/             Aprovisionamiento de schemas/tablas
  query/                   Query builder SQL (a prueba de inyección)
  registry/                Registro de apps en memoria, thread-safe
  server/                  Router HTTP, handlers, middleware
  sshkey/                  Generación de par de claves ED25519 (OpenSSH nativo)
charts/                    Helm chart
k8s/                       Manifiestos Kustomize
clients/                   SDK clients (TS, Go, Python, Rust, Java, PHP)
examples/                  Apps de ejemplo (Todo app)
```

---

## 🤝 Contribuir

Consulta [CONTRIBUTING.md](../CONTRIBUTING.md). Toda contribución es bienvenida — correcciones, funcionalidades, documentación, tests.

---

## 📄 Licencia

MIT — ver [LICENSE](../LICENSE).

---

## 🏢 Sobre Zeep Tecnologia

Zeep Orbit fue creado por [Zeep Tecnologia](https://zeeptecnologia.com.br) para resolver algo que veíamos en todas partes: equipos usando herramientas de IA para crear frontends en minutos — y quedándose atascados cuando necesitan un backend y un despliegue.

Levantar una base de datos, escribir migraciones, desplegar una API, gestionar auth, manejar secrets, configurar dominios, montar CI/CD — todo eso mata el impulso. Y las alternativas envían tus datos e infraestructura fuera de tu control.

Zeep Orbit es nuestra respuesta: **un solo binario, tu PostgreSQL, apps infinitas.** Despliega en tu propia infraestructura, conecta cualquier frontend, avanza rápido sin el overhead.

Construimos infraestructura open-source para la era de la IA. [Únete a nosotros](https://github.com/zeeplabs/zeep-orbit/discussions).
