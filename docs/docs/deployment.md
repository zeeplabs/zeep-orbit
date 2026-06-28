---
sidebar_position: 7
---

# Deployment

## Docker (standalone)

```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:latest

docker run -d \
  --name zeep-orbit \
  -p 8080:8080 \
  -e DATABASE_URL=postgres://user:pass@host:5432/db \
  -e DASHBOARD_BOOTSTRAP_SECRET=my-secret \
  ghcr.io/zeeplabs/zeep-orbit:latest
```

> Se o PostgreSQL estiver rodando na máquina host, use `host.docker.internal` no lugar de `localhost`.

## EasyPanel

Zeep Orbit pode ser instalado no EasyPanel como **App - Docker Image**.

### Passo a passo

1. No EasyPanel, vá em **Aplicativos** → **Criar Aplicativo**
2. Escolha **Docker Image**
3. Preencha os campos:

| Campo | Valor |
|-------|-------|
| Nome | `zeep-orbit` |
| Imagem | `ghcr.io/zeeplabs/zeep-orbit:latest` |
| Porta | `8080` |
| Protocolo | `HTTP` |

4. Em **Ambiente (Environment)**, adicione:

```env
DATABASE_URL=postgres://user:pass@postgres:5432/zeep
DASHBOARD_BOOTSTRAP_SECRET=sua-chave-secreta-aqui
```

5. Em **Banco de Dados**, adicione um **PostgreSQL** como serviço dependente
6. Crie o aplicativo

A URL do PostgreSQL geralmente segue o padrão:
```
postgres://<projeto>_<usuario>:<senha>@postgres:5432/<projeto>_<database>
```

### Usando Google OAuth (opcional)

Se quiser ativar login via Google no dashboard, adicione também:

```env
GOOGLE_CLIENT_ID=seu-client-id
GOOGLE_CLIENT_SECRET=seu-client-secret
GOOGLE_REDIRECT_URL=https://seu-dominio.com/dashboard/api/auth/google/callback
```

> As credenciais Google também podem ser configuradas diretamente pela interface do dashboard em **Configurações → Google OAuth**.

## Kubernetes (Helm)

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit \
  --set secrets.databaseUrl=postgres://... \
  --set secrets.dashboardBootstrapSecret=my-secret \
  --set 'secrets.apps.myapp.jwtSecret=...'
```

## Docker Compose

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
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```
