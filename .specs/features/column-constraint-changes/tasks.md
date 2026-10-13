# Column Constraint Changes — Tasks

| # | Task | Requisitos | Verificação | Status |
|---|---|---|---|---|
| T1 | `DiffColumnConstraints` + `CheckColumnConstraintChanges` + `ApplyColumnConstraintChanges` (transacional, idempotente) e `*ColumnConstraintError` | CCC-01..06 | `internal/provisioner/column_constraints_test.go` | Done |
| T2 | `addMissingColumns` rejeita coluna nova required sem default em tabela não vazia com erro tipado | CCC-04 | `TestAddMissingColumns_RequiredWithoutDefaultOnNonEmptyTable` | Done |
| T3 | `UpdateAppTable`: pré-check → `Apply` → constraints → persistir; `writeProvisioningError` | CCC-01, CCC-04, CCC-07 | `internal/dashboard/apps_column_constraints_handler_test.go` | Done |
| T4 | `ColumnConstraintError` mapeado em create app/table REST, AI build/edit chat e MCP `mapWriteError` | CCC-07 | `go build`, suites `dashboard`/`mcpserver` | Done |
| T5 | `classifyWriteError` em create/update/delete da Data API + log de 500 restante | ERR-01, ERR-02 | `internal/server/write_errors_test.go`, testes de RLS/22021 atualizados | Done |
| T6 | `query.NormalizeInteger` (fração/fora da faixa em integer/bigint → 400 nomeando a coluna) | ERR-01 | `TestNormalizeInteger`, `TestBuildInsertRejectsOutOfRangeInteger` | Done |
| T7 | Docs: CHANGELOG, OpenAPI (`internal/docs/generator.go`), referência REST do skill `orbit-usage` | — | revisão | Done |
| T8 | Corrigir desvios já existentes em produção (`asset-manager.employees.cpf/email`, `employees.status`, `github-dev-activity.backfill_progress.last_synced_at`) | fora do código | aprovação caso a caso do Julio | Pending |
