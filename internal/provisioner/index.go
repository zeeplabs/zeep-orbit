package provisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// ensureIndexes creates any indexes declared on the table that don't already
// exist. Idempotent via IF NOT EXISTS. Never drops an index that was removed
// from the YAML — an index disappearing from config is not treated as
// intent to drop it, since that's a destructive, easy-to-trigger-by-accident
// operation.
func (p *Provisioner) ensureIndexes(ctx context.Context, schemaName, tableName string, indexes []config.IndexConfig) ([]string, error) {
	var created []string

	for _, idx := range indexes {
		quotedCols := make([]string, len(idx.Columns))
		for i, c := range idx.Columns {
			quotedCols[i] = fmt.Sprintf("%q", c)
		}

		uniqueClause := ""
		if idx.Unique {
			uniqueClause = "UNIQUE "
		}

		sql := fmt.Sprintf(
			`CREATE %sINDEX IF NOT EXISTS %q ON %q.%q (%s)`,
			uniqueClause, idx.Name, schemaName, tableName, strings.Join(quotedCols, ", "),
		)

		if _, err := p.pool.Exec(ctx, sql); err != nil {
			return nil, fmt.Errorf("index: create %q on %q.%q: %w", idx.Name, schemaName, tableName, err)
		}

		created = append(created, fmt.Sprintf("%s.%s.%s", schemaName, tableName, idx.Name))
	}

	return created, nil
}
