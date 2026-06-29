package provisioner

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Provisioner applies schemas and tables for all apps defined in the config.
type Provisioner struct {
	pool *db.Pool
}

// New creates a Provisioner linked to the provided connection pool.
func New(pool *db.Pool) *Provisioner {
	return &Provisioner{pool: pool}
}

// Report describes what was created or changed during an Apply.
type Report struct {
	SchemasCreated  []string
	TablesCreated   []string
	ColumnsAdded    []string
	ColumnsChanged  []string
}

// Idempotent: safe to call multiple times with no side effects.
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

		if app.Storage != nil && app.Storage.Bucket != "" {
			storageCreated, err := p.provisionStorageTables(ctx, schemaName)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q storage tables: %w", app.Name, err)
			}
			report.TablesCreated = append(report.TablesCreated, storageCreated...)
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
