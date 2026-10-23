package provisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Provisioner applies schemas and tables for all apps defined in the config.
//
// Every exported mutating method runs in one transaction that first takes
// the per-app DDL lock (db.LockAppDDL): DDL for one app is serialized across
// replicas, every lock wait is bounded by db.DDLLockTimeout, and a failure
// halfway through rolls back every statement, so no partial DDL is left.
type Provisioner struct {
	q querier
	// locked is true for the transaction-scoped copy withAppLock hands to
	// fn, so a locked method calling another one doesn't nest the lock.
	locked bool
}

// querier is what the provisioner runs against: the pool, or the
// transaction opened by withAppLock (where Begin becomes a savepoint).
type querier interface {
	db.Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// New creates a Provisioner linked to the provided connection pool.
func New(pool *db.Pool) *Provisioner {
	return &Provisioner{q: pool}
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
// Each app runs in its own transaction under its per-app DDL lock; an error
// rolls back that app's changes entirely (earlier apps stay committed).
func (p *Provisioner) Apply(ctx context.Context, cfg *config.Config) (*Report, error) {
	report := &Report{}

	for _, app := range cfg.Apps {
		schemaName := strings.ReplaceAll(app.Name, "-", "_")
		appReport := &Report{}
		if err := p.withAppLock(ctx, schemaName, func(tp *Provisioner) error {
			return tp.applyApp(ctx, app, schemaName, appReport)
		}); err != nil {
			return nil, err
		}
		report.merge(appReport)
	}

	return report, nil
}

func (r *Report) merge(o *Report) {
	r.SchemasCreated = append(r.SchemasCreated, o.SchemasCreated...)
	r.TablesCreated = append(r.TablesCreated, o.TablesCreated...)
	r.ColumnsAdded = append(r.ColumnsAdded, o.ColumnsAdded...)
	r.ColumnsChanged = append(r.ColumnsChanged, o.ColumnsChanged...)
	r.IndexesCreated = append(r.IndexesCreated, o.IndexesCreated...)
}

func (p *Provisioner) applyApp(ctx context.Context, app config.AppConfig, schemaName string, report *Report) error {
	created, err := p.createSchema(ctx, schemaName)
	if err != nil {
		return fmt.Errorf("provisioner: app %q: %w", app.Name, err)
	}
	if created {
		report.SchemasCreated = append(report.SchemasCreated, schemaName)
	}

	if app.Auth.Providers.Email {
		authCreated, err := p.provisionAuthTables(ctx, schemaName)
		if err != nil {
			return fmt.Errorf("provisioner: app %q auth tables: %w", app.Name, err)
		}
		report.TablesCreated = append(report.TablesCreated, authCreated...)
	}

	if app.Storage != nil && app.Storage.Bucket != "" {
		storageCreated, err := p.provisionStorageTables(ctx, schemaName)
		if err != nil {
			return fmt.Errorf("provisioner: app %q storage tables: %w", app.Name, err)
		}
		report.TablesCreated = append(report.TablesCreated, storageCreated...)
	}

	orderedTables, err := topoSortTables(app.Tables)
	if err != nil {
		return fmt.Errorf("provisioner: app %q: %w", app.Name, err)
	}

	for _, table := range orderedTables {
		tableCreated, err := p.createTable(ctx, schemaName, table.Name, table.Columns, table.RLS)
		if err != nil {
			return fmt.Errorf("provisioner: app %q table %q: %w", app.Name, table.Name, err)
		}
		if !tableCreated {
			changed, err := p.applyColumnChanges(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return fmt.Errorf("provisioner: app %q table %q apply changes: %w", app.Name, table.Name, err)
			}
			report.ColumnsChanged = append(report.ColumnsChanged, changed...)

			added, err := p.addMissingColumns(ctx, schemaName, table.Name, table.Columns, table.RLS)
			if err != nil {
				return fmt.Errorf("provisioner: app %q table %q add columns: %w", app.Name, table.Name, err)
			}
			report.ColumnsAdded = append(report.ColumnsAdded, added...)
		} else {
			report.TablesCreated = append(report.TablesCreated, fmt.Sprintf("%s.%s", schemaName, table.Name))
		}

		indexesCreated, err := p.ensureIndexes(ctx, schemaName, table.Name, table.Indexes)
		if err != nil {
			return fmt.Errorf("provisioner: app %q table %q indexes: %w", app.Name, table.Name, err)
		}
		report.IndexesCreated = append(report.IndexesCreated, indexesCreated...)
	}
	return nil
}

// ensureAuthTables is the body of EnsureAuthTables, run under the per-app DDL lock.
func (p *Provisioner) ensureAuthTables(ctx context.Context, schemaName string) error {
	if _, err := p.createSchema(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q: %w", schemaName, err)
	}
	if _, err := p.provisionAuthTables(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q auth tables: %w", schemaName, err)
	}
	return nil
}

// ensureStorageTables is the body of EnsureStorageTables, run under the per-app DDL lock.
func (p *Provisioner) ensureStorageTables(ctx context.Context, schemaName string) error {
	if _, err := p.createSchema(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q: %w", schemaName, err)
	}
	if _, err := p.provisionStorageTables(ctx, schemaName); err != nil {
		return fmt.Errorf("provisioner: schema %q storage tables: %w", schemaName, err)
	}
	return nil
}
