package provisioner

import (
	"context"
	"fmt"
)

// (did not exist before), false if it already existed.
func (p *Provisioner) createSchema(ctx context.Context, schemaName string) (bool, error) {
	// Checks existence first to detect if it was just created.
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`,
		schemaName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("schema: check existence %q: %w", schemaName, err)
	}

	if exists {
		return false, nil
	}

	_, err = p.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schemaName))
	if err != nil {
		return false, fmt.Errorf("schema: create %q: %w", schemaName, err)
	}

	return true, nil
}
