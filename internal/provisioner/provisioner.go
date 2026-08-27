package provisioner

import (
	"context"
	"fmt"
	"strings"

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
	SchemasCreated []string
	TablesCreated  []string
	ColumnsAdded   []string
	ColumnsChanged []string
	IndexesCreated []string
}

// Idempotent: safe to call multiple times with no side effects.
func (p *Provisioner) Apply(ctx context.Context, cfg *config.Config) (*Report, error) {
	report := &Report{}

	for _, app := range cfg.Apps {
		schemaName := strings.ReplaceAll(app.Name, "-", "_")

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

		orderedTables, err := topoSortTables(app.Tables)
		if err != nil {
			return nil, fmt.Errorf("provisioner: app %q: %w", app.Name, err)
		}

		for _, table := range orderedTables {
			tableCreated, err := p.createTable(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q table %q: %w", app.Name, table.Name, err)
			}
			if !tableCreated {
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
			} else {
				report.TablesCreated = append(report.TablesCreated, fmt.Sprintf("%s.%s", schemaName, table.Name))
			}

			indexesCreated, err := p.ensureIndexes(ctx, schemaName, table.Name, table.Indexes)
			if err != nil {
				return nil, fmt.Errorf("provisioner: app %q table %q indexes: %w", app.Name, table.Name, err)
			}
			report.IndexesCreated = append(report.IndexesCreated, indexesCreated...)
		}
	}

	return report, nil
}

// EnsureAuthTables provisions the "_auth_users"/"_auth_sessions" tables for
// one app's schema, without touching any other app or table. Idempotent —
// safe to call every time an app's auth_email_enabled flips to true,
// including when it was already true. Use this instead of Apply when only
// auth needs provisioning: Apply reconciles every table in the app's config,
// which is the wrong blast radius for a request that only toggles auth.
func (p *Provisioner) EnsureAuthTables(ctx context.Context, schemaName string) error {
	if _, err := p.createSchema(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q: %w", schemaName, err)
	}
	if _, err := p.provisionAuthTables(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q auth tables: %w", schemaName, err)
	}
	return nil
}

// EnsureStorageTables provisions the "_files" table for one app's schema,
// without touching any other app or table. Idempotent — safe to call every
// time an app's storage bucket is set, including when it was already set.
// Use this instead of Apply for the same reason as EnsureAuthTables.
func (p *Provisioner) EnsureStorageTables(ctx context.Context, schemaName string) error {
	if _, err := p.createSchema(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q: %w", schemaName, err)
	}
	if _, err := p.provisionStorageTables(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q storage tables: %w", schemaName, err)
	}
	return nil
}
