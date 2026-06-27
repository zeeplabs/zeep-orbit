package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool para permitir extensão futura sem quebrar o contrato externo.
type Pool struct {
	*pgxpool.Pool
}

// New cria e valida um pool de conexões PostgreSQL.
// dsn deve estar no formato postgres://user:pass@host:port/db (DATABASE_URL).
// O ctx é usado durante a conexão inicial e o Ping; use context.WithTimeout para
// limitar o tempo de espera (recomendado: 5s).
func New(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: invalid DSN: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping failed: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// Close encerra todas as conexões do pool.
func (p *Pool) Close() {
	p.Pool.Close()
}
