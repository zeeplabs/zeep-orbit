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
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit \
  --set secrets.databaseUrl=postgres://... \
  --set secrets.dashboardBootstrapSecret=my-secret \
  --set 'secrets.apps.myapp.jwtSecret=...'
```

## Várias réplicas

O Orbit roda com várias réplicas atrás de um load balancer sem sticky session (o chart usa `replicaCount: 2` por padrão). O que cada réplica precisa saber:

### Propagação de mudanças

Cada pod guarda em memória os apps, as tabelas e a configuração de sistema. Toda escrita que muda esse estado (dashboard, MCP, chat de IA ou SQL manual em `zeep_system`) incrementa `zeep_system.state_version` via trigger. Cada pod lê essa versão a cada 2s e recarrega quando ela muda.

- Uma mudança feita em um pod chega aos outros em até ~2s.
- Se um pod recebe uma request para um app ou tabela que ainda não conhece, ele sincroniza na hora (no máximo uma vez por segundo) antes de responder 404.
- Revogar um token ou rotacionar o `jwt_secret` de um app vale em todos os pods dentro da mesma janela.
- O boot falha se a configuração inicial não puder ser carregada. Antes, o pod subia sem statement timeout e com soft delete desligado.

### Probes

- `/health` (liveness) não acessa o banco. Um banco fora do ar não reinicia os pods.
- `/ready` (readiness) faz um ping no banco com timeout de 2s e responde 503 se falhar. O pod sai do Service até o banco voltar.

O chart e `k8s/deployment.yaml` já usam `/ready` na readiness.

### Conexões

Cada pod abre até `DB_MAX_CONNS` conexões (padrão 10; no chart, `database.maxConns`), mais uma dedicada ao readiness probe e até 4 do pool de deduplicação de webhooks (aberto no primeiro webhook com deduplicação). Mantenha `réplicas × (DB_MAX_CONNS + 5)`, contando o máximo do HPA, abaixo do `max_connections` do Postgres, descontando o que outros clientes usam.

### Mudanças de schema (DDL)

Criar ou editar tabelas, colunas, índices, FKs, RLS e policies roda em uma transação por app, com um lock por app compartilhado entre réplicas. A espera por lock é limitada a 5s. Se uma query longa ou uma transação aberta segurar a tabela, a API responde `409` com `table is busy with another operation, try again in a few seconds` e nada é alterado. O log registra quem segurava o lock (pid, estado, tipo de backend, horários e modo do lock; nunca o texto da query, o usuário ou o IP).

### Rate limit

O rate limit por app é por pod. Com N réplicas, o limite efetivo é até N × o valor configurado.

### Runbook: criação ou edição de tabelas travada

Rode isto **antes** de reiniciar os pods. O restart encerra a sessão que segura o lock e apaga a evidência.

1. Veja se o problema é de um pod ou de todos. Teste cada pod direto (o label abaixo é o do chart; com os manifestos de `k8s/`, use `-l app=zeep-orbit`):

   ```bash
   kubectl get pods -l app.kubernetes.io/name=zeep-orbit
   kubectl port-forward pod/<pod> 18080:8080
   curl -s localhost:18080/health   # apps carregados neste pod
   curl -s localhost:18080/ready
   ```

   Se só um pod diverge (contagem de apps diferente, 404 em um app que existe), confira se a imagem é a mesma em todos:

   ```bash
   kubectl get pods -l app.kubernetes.io/name=zeep-orbit \
     -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.status.containerStatuses[0].imageID}{"\n"}{end}'
   ```

2. Procure quem está bloqueando, com um usuário com permissão de ver todas as sessões:

   ```sql
   SELECT pid, state, backend_type, xact_start, state_change, wait_event_type,
          pg_blocking_pids(pid) AS blocked_by, left(query, 80) AS query
   FROM pg_stat_activity
   WHERE datname = current_database()
     AND (cardinality(pg_blocking_pids(pid)) > 0 OR state = 'idle in transaction')
   ORDER BY xact_start;
   ```

   - `blocked_by` mostra os pids que seguram o lock.
   - Sessões `idle in transaction` com `xact_start` antigo são as suspeitas mais comuns: uma transação aberta e esquecida segura locks indefinidamente.

3. Se houver um pooler (PgBouncer ou similar) entre o Orbit e o Postgres, rode a mesma consulta no Postgres, não no pooler, e confira o modo do pool. Os locks de DDL do Orbit são por transação e funcionam em modo transaction.

4. Encerre só a sessão que bloqueia, depois de entender o que ela é:

   ```sql
   SELECT pg_cancel_backend(<pid>);     -- cancela a query
   SELECT pg_terminate_backend(<pid>);  -- encerra a sessão, se o cancel não bastar
   ```

5. Repita a operação no dashboard. Se ainda travar, colete os logs de `ddl lock timeout` dos pods antes de reiniciar qualquer coisa.

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
