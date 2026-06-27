package provisioner

import (
	"context"
	"fmt"
)

// createSchema executa CREATE SCHEMA IF NOT EXISTS e retorna true se o schema foi criado
// (não existia antes), false se já existia.
func (p *Provisioner) createSchema(ctx context.Context, schemaName string) (bool, error) {
	// Verifica existência antes para detectar se foi criado agora.
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

	// Identificadores DDL são validados pelo config (T-002); fmt.Sprintf é seguro aqui.
	_, err = p.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schemaName))
	if err != nil {
		return false, fmt.Errorf("schema: create %q: %w", schemaName, err)
	}

	return true, nil
}
