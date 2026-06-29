package provisioner

import (
	"context"
	"fmt"
)

func (p *Provisioner) provisionStorageTables(ctx context.Context, schema string) ([]string, error) {
	var created []string

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q."_files" (
		"id"         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
		"name"       TEXT        NOT NULL,
		"size"       BIGINT      NOT NULL,
		"mime_type"  TEXT        NOT NULL DEFAULT 'application/octet-stream',
		"key"        TEXT        NOT NULL UNIQUE,
		"owner_id"   UUID,
		"created_at" TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, schema)

	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = '_files')`,
		schema,
	).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("storage: check _files existence %q: %w", schema, err)
	}

	if !exists {
		if _, err := p.pool.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("storage: create _files in %q: %w", schema, err)
		}
		created = append(created, schema+"._files")
	}

	return created, nil
}
