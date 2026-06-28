// Package provisioner manages PostgreSQL schema and table provisioning.
// It creates app-specific schemas, auth tables, and CRUD tables idempotently
// and handles column migrations (add, rename, type change) safely.
package provisioner

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Provisioner aplica schemas e tabelas para todos os apps definidos no config.
type Provisioner struct {
	pool *db.Pool
}

// New cria um Provisioner vinculado ao pool de conexões fornecido.
func New(pool *db.Pool) *Provisioner {
	return &Provisioner{pool: pool}
}

// Report descreve o que foi criado ou alterado durante um Apply.
type Report struct {
	SchemasCreated  []string
	TablesCreated   []string
	ColumnsAdded    []string // formato: "schema.table.column"
	ColumnsChanged  []string // formato: "schema.table.column (descrição)"
}

// Apply provisiona todos os apps: cria schemas, tabelas e adiciona colunas ausentes.
// Também aplica migrations (renomeios e mudanças de tipo) em tabelas existentes.
// É idempotente: pode ser chamado múltiplas vezes sem efeito colateral.
func (p *Provisioner) Apply(ctx context.Context, cfg *config.Config) (*Report, error) {
	report := &Report{}

	for _, app := range cfg.Apps {
		schemaName := app.Name

		created, err := p.createSchema(ctx, schemaName)
		if err != nil {
			return nil, fmt.Errorf("provisioner: app %q: %w", app.Name, err)
		}
		if created {
			report.SchemasCreated = append(report.SchemasCreated, schemaName)
		}

		if app.Auth.Providers.Email {
			authCreated, err := p.provisionAuthTables(ctx, schemaName)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q auth tables: %w", app.Name, err)
			}
			report.TablesCreated = append(report.TablesCreated, authCreated...)
		}

		for _, table := range app.Tables {
			tableCreated, err := p.createTable(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q table %q: %w", app.Name, table.Name, err)
			}
			if tableCreated {
				report.TablesCreated = append(report.TablesCreated, fmt.Sprintf("%s.%s", schemaName, table.Name))
				continue
			}

			// Tabela já existia — primeiro aplica renomeios e mudanças de tipo,
			// depois adiciona colunas realmente novas.
			changed, err := p.applyColumnChanges(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q table %q apply changes: %w", app.Name, table.Name, err)
			}
			report.ColumnsChanged = append(report.ColumnsChanged, changed...)

			added, err := p.addMissingColumns(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q table %q add columns: %w", app.Name, table.Name, err)
			}
			report.ColumnsAdded = append(report.ColumnsAdded, added...)
		}
	}

	return report, nil
}
