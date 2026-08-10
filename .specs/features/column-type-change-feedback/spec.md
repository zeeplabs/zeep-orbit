# Column Type Change Feedback Specification

## Problem Statement

`applyTypeChange` (`internal/provisioner/migration.go:117-167`) já bloqueia conversões de tipo "narrowing" (ex.: `text→integer`) via uma allow-list estática (`safeTypeConversions`, linhas 13-22) antes de rodar qualquer SQL, retornando um erro Go razoável tipo "unsafe conversion (would narrow or lose data)". Isso cobre o caso mais óbvio. Mas duas lacunas reais permanecem:

1. **A allow-list é por família de tipo, não por dado real da coluna.** Uma conversão "segura" na lista (ex.: `int4→text`, `text` não está na lista mas hipoteticamente qualquer conversão liberada) é enviada direto ao Postgres via `ALTER TABLE ... ALTER COLUMN ... TYPE ... USING col::type` (linha 151-152). Se essa conversão falhar em runtime por qualquer motivo não coberto pela lista estática — dado incompatível dentro do `USING` cast, lock, permissão — o erro cru do driver Postgres (SQLSTATE + mensagem técnica) propaga sem tradução.
2. **Esse erro cru vaza pro usuário final sem filtro.** Os 4 pontos de entrada em `internal/dashboard/handler.go` (linhas 813, 909, 997, 1073) fazem `h.writeError(w, r, http.StatusInternalServerError, "provisioning failed: "+err.Error(), err)` — concatenam `err.Error()` bruto (que inclui a cadeia `%w` de `applyColumnChanges` → `applyTypeChange` → erro pgx original) direto no corpo JSON da resposta HTTP, diferente de outros handlers do mesmo arquivo que usam mensagem genérica de erro interno.

Resultado: pra narrowing óbvio já existe feedback (não é o "zero feedback" hipotético inicial), mas pra qualquer falha fora da allow-list o usuário recebe erro técnico de banco (SQLSTATE, sintaxe SQL) em vez de mensagem acionável — e não há como saber, antes de tentar aplicar, se uma mudança de tipo é seguros.

## Goals

- [x] Usuário recebe mensagem clara e acionável (não erro cru de driver) para qualquer falha de mudança de tipo de coluna, não só as já cobertas pela allow-list
- [x] Erro genérico interno (`err.Error()` bruto) para de vazar na resposta HTTP dos 4 handlers afetados
- [x] Allow-list revisada/documentada para refletir exatamente o que o PostgreSQL suporta via `USING` cast, reduzindo o conjunto de "conversões supostamente seguras que falham em runtime"
- [x] Mensagem de erro (allow-list ou runtime) indica a coluna, tipo atual, tipo desejado e motivo em linguagem não-técnica

## Out of Scope

| Feature | Reason |
|---|---|
| Dry-run real com introspecção do dado da coluna (ex.: `SELECT` prévio pra validar se todo valor casta) | Custo de performance em tabela grande sem garantia de cobrir 100% dos casos (dado pode mudar entre o dry-run e o apply); resolvido nesta spec por tradução de erro melhor, não por validação de dado real |
| Migração automática de dado incompatível (ex.: truncar, converter valor por valor com fallback) | Decisão de dado é do usuário, sistema não deve silenciosamente perder informação |
| UI de preview "isso vai quebrar X linhas" no dashboard | Depende de dry-run real (fora de escopo); pode ser iteração futura se pedido |
| Revisão de erro de outras operações de schema (rename, add column, drop table) | Esta spec foca especificamente em type change; outros fluxos de erro ficam para spec própria se necessário |

---

## User Stories

### P1: Usuário recebe mensagem clara quando type change é rejeitado pela allow-list ⭐ MVP

**User Story**: Como usuário tentando mudar o tipo de uma coluna, quero uma mensagem que diga claramente qual coluna, de qual tipo pra qual tipo, e por que foi rejeitado, para eu decidir o que fazer sem precisar interpretar erro de banco.

**Why P1**: Já existe uma mensagem razoável (`migration.go:134,146`), mas ela não chega ao usuário formatada de forma consistente — é concatenada com prefixo técnico "provisioning failed: " e a cadeia de erro Go completa. Garantir que ela chegue limpa é a base de tudo.

**Acceptance Criteria**:

1. WHEN uma mudança de tipo é rejeitada pela allow-list (`safeTypeConversions`) THEN a resposta HTTP SHALL conter mensagem clara em inglês (mesmo idioma/tom do resto de `handler.go`), no formato "cannot change `{column}` from `{current_type}` to `{desired_type}`: {motivo claro}", sem prefixo redundante ("provisioning failed:") e sem a cadeia de erro Go (`%w`) exposta.
2. WHEN o tipo de origem não tem nenhuma conversão definida na allow-list (`migration.go:132-136`, ex. `text`) THEN a mensagem SHALL dizer explicitamente que esse tipo de origem não permite conversão automática, não só "no defined conversions".
3. WHEN o tipo de destino não está entre os permitidos pro tipo de origem THEN a mensagem SHALL indicar que a conversão perderia dado (narrowing), consistente com o motivo real.

**Independent Test**: Tentar mudar coluna `text` para `integer` via API; resposta HTTP 4xx/5xx (a definir no design) tem mensagem legível citando coluna e tipos, sem string de erro Go bruta (`%!w`, `pgx:`, SQLSTATE) no corpo.

---

### P1: Erro de runtime do Postgres (fora da allow-list) não vaza cru pro usuário ⭐ MVP

**User Story**: Como usuário, quero que uma falha inesperada do banco durante mudança de tipo me diga "a mudança falhou, tente algo" em vez de me mostrar código SQL e SQLSTATE, para eu não ficar bloqueado sem saber o que fazer.

**Why P1**: É o vazamento real identificado — `writeError` recebe `err.Error()` bruto nos 4 call sites (`handler.go:813,909,997,1073`), diferente do padrão do resto do arquivo.

**Acceptance Criteria**:

1. WHEN `p.pool.Exec` falha ao rodar o `ALTER TABLE ... TYPE ... USING` (linha 154 de `migration.go`) por qualquer motivo não capturado pela allow-list THEN a mensagem pública HTTP SHALL ser uma mensagem tratada em inglês (ex.: "failed to change type of `{column}` — check that existing data is compatible with the new type"), não o `err.Error()` bruto.
2. WHEN o erro de runtime ocorre THEN o erro técnico completo (SQLSTATE, mensagem pgx, SQL executado) SHALL continuar sendo logado server-side (via o parâmetro `err` já passado pra `writeError`, que hoje já loga — comportamento existente preservado, só a mensagem pública muda).
3. WHEN outros tipos de erro de provisioning (não relacionados a type change) passam pelos mesmos 4 call sites THEN o comportamento de mensagem genérica SHALL também ser aplicado — não é uma exceção só pra type change, é o padrão a ser corrigido nesses 4 pontos.

**Independent Test**: Forçar erro de runtime não coberto pela allow-list (ex.: mock ou cenário de dado incompatível dentro de uma conversão "segura" na lista, se existir algum caso — a validar durante design); resposta HTTP não contém texto de erro pgx cru nem SQL.

---

### P2: Allow-list revisada para refletir realidade de casts do PostgreSQL

**User Story**: Como usuário, quero que o sistema só chame "seguro" uma conversão que o PostgreSQL realmente executa sem erro na maioria dos casos, para eu confiar na ausência de erro como sinal de que a mudança vai funcionar.

**Why P2**: Reduz a superfície do problema P1 (menos erro de runtime acontece), mas não é bloqueante — mesmo com allow-list perfeita, dado incompatível dentro de um tipo "seguro" sempre pode ocorrer (ex.: `text→integer` bloqueado, mas nada impede um `int4` com valor fora do range de outro tipo age não-óbvio). Tratamento de erro (P1) resolve o problema de fundo independente da qualidade da lista.

**Acceptance Criteria**:

1. WHEN a allow-list é revisada THEN cada entrada SHALL ter uma justificativa documentada (comentário no código) do porquê aquela conversão é considerada segura, citando comportamento real do `USING col::type` do PostgreSQL.
2. WHEN uma conversão comum e razoavelmente segura está ausente da lista atual (ex.: `int4→int8` sem estar listada — hoje só `int4→{int8,numeric,text}` já cobre isso, mas revisar todos os pares de `pgTypeToUDT`, linha 24-46) THEN ela SHALL ser avaliada e adicionada se de fato segura.

**Independent Test**: Revisão de código/PR mostra tabela de tipos suportados por `pgTypeToUDT` (`int4, int8, numeric, text, bool, uuid, timestamptz, jsonb`) cruzada com a allow-list, com todo par ausente justificado explicitamente (seguro mas não crítico agora / inseguro por definição).

---

## Constraints Técnicas

- Mensagem pública nunca deve conter SQLSTATE, nome de driver (`pgx`), ou o SQL literal executado — isso é log server-side, não resposta de API.
- Erro técnico completo continua sendo passado pro parâmetro `err` de `writeError` pra fins de log/observabilidade — não há perda de informação para debugging interno, só filtragem do que é exposto externamente.
- Mensagens públicas seguem o idioma e tom já usado em `handler.go` (inglês, curto, ex.: `"failed to resolve storage config"`) — não introduzir português nas mensagens de erro só porque esta spec foi escrita em português.
