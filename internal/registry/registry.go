package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// All read and write operations are protected by RWMutex.
type Registry struct {
	mu   sync.RWMutex
	apps map[string]*App
}

// App represents an application with its schema and tables.
type App struct {
	Config        config.AppConfig
	SchemaName    string
	Tables        map[string]*Table
	AuthProviders map[string]any
	StorageConfig *config.StorageConfig
	RateLimit     *config.RateLimitConfig
}

// Table representa uma tabela dentro de um app.
type Table struct {
	Name    string
	RLS     string
	Columns []Column
}

// Column representa uma coluna de uma tabela.
type Column struct {
	Name       string
	Type       string
	Required   bool
	Default    string
	Unique     bool
	RenameFrom string
}

// New retorna um Registry vazio, pronto para uso.
func New() *Registry {
	return &Registry{
		apps: make(map[string]*App),
	}
}

// Retorna erro se algum app tiver Name vazio.
func (r *Registry) Load(cfg *config.Config) error {
	newApps := make(map[string]*App, len(cfg.Apps))

	for _, appCfg := range cfg.Apps {
		if appCfg.Name == "" {
			return fmt.Errorf("registry: app com Name vazio encontrado na configuração")
		}

		tables := make(map[string]*Table, len(appCfg.Tables))
		for _, tblCfg := range appCfg.Tables {
			cols := make([]Column, 0, len(tblCfg.Columns))
			for _, colCfg := range tblCfg.Columns {
				cols = append(cols, Column{
					Name:     colCfg.Name,
					Type:     colCfg.Type,
					Required: colCfg.Required,
					Default:  colCfg.Default,
					Unique:   colCfg.Unique,
					RenameFrom: colCfg.RenameFrom,
				})
			}
			tables[tblCfg.Name] = &Table{
				Name:    tblCfg.Name,
				RLS:     tblCfg.RLS,
				Columns: cols,
			}
		}

		newApps[appCfg.Name] = &App{
			Config:     appCfg,
			SchemaName: appCfg.Name,
			Tables:     tables,
		}
	}

	r.mu.Lock()
	r.apps = newApps
	r.mu.Unlock()

	return nil
}

// O segundo valor indica se o app foi encontrado.
func (r *Registry) Get(appName string) (*App, bool) {
	r.mu.RLock()
	app, ok := r.apps[appName]
	r.mu.RUnlock()
	return app, ok
}

// Returns (nil, false) if the app or table does not exist.
func (r *Registry) GetTable(appName, tableName string) (*Table, bool) {
	r.mu.RLock()
	app, ok := r.apps[appName]
	r.mu.RUnlock()

	if !ok {
		return nil, false
	}

	tbl, ok := app.Tables[tableName]
	return tbl, ok
}

// Returns a copy of the slice without exposing the internal map.
func (r *Registry) Apps() []*App {
	r.mu.RLock()
	result := make([]*App, 0, len(r.apps))
	for _, app := range r.apps {
		result = append(result, app)
	}
	r.mu.RUnlock()
	return result
}

// Replaces any existing state. Safe to call on startup.
func (r *Registry) LoadFromDB(ctx context.Context, pool *db.Pool) error {
		type appRow struct {
			id               string
			name             string
			jwtSecret        string
			authEmailEnabled bool
			authProviders    []byte
			storageConfig    []byte
			rateLimitConfig  []byte
		}

		rows, err := pool.Query(ctx,
			`SELECT id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), COALESCE(storage_config, '{}'), COALESCE(rate_limit_config, '{}') FROM zeep_system.apps ORDER BY name`,
		)
	if err != nil {
		return fmt.Errorf("registry: load from db: query apps: %w", err)
	}
	defer rows.Close()

		var appRows []appRow
		for rows.Next() {
			var a appRow
			var providersJSON, storageJSON, rateLimitJSON []byte
			if err := rows.Scan(&a.id, &a.name, &a.jwtSecret, &a.authEmailEnabled, &providersJSON, &storageJSON, &rateLimitJSON); err != nil {
				return fmt.Errorf("registry: load from db: scan app: %w", err)
			}
			a.authProviders = providersJSON
			a.storageConfig = storageJSON
			a.rateLimitConfig = rateLimitJSON
			appRows = append(appRows, a)
		}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("registry: load from db: rows: %w", err)
	}

	newApps := make(map[string]*App, len(appRows))

	for _, a := range appRows {
		tableRows, err := pool.Query(ctx,
			`SELECT name, rls, columns FROM zeep_system.app_tables WHERE app_id = $1 ORDER BY name`,
			a.id,
		)
		if err != nil {
			return fmt.Errorf("registry: load from db: query tables for %s: %w", a.name, err)
		}

		tables := make(map[string]*Table)
		var tableCfgs []config.TableConfig

		for tableRows.Next() {
			var tName, tRLS string
			var colsJSON []byte
			if err := tableRows.Scan(&tName, &tRLS, &colsJSON); err != nil {
				tableRows.Close()
				return fmt.Errorf("registry: load from db: scan table: %w", err)
			}

			var cols []config.ColumnConfig
			if err := json.Unmarshal(colsJSON, &cols); err != nil {
				tableRows.Close()
				return fmt.Errorf("registry: load from db: unmarshal columns for %s.%s: %w", a.name, tName, err)
			}

			regCols := make([]Column, 0, len(cols))
			for _, c := range cols {
				regCols = append(regCols, Column{
					Name:     c.Name,
					Type:     c.Type,
					Required: c.Required,
					Default:  c.Default,
					Unique:   c.Unique,
					RenameFrom: c.RenameFrom,
				})
			}

			tables[tName] = &Table{Name: tName, RLS: tRLS, Columns: regCols}
			tableCfgs = append(tableCfgs, config.TableConfig{Name: tName, RLS: tRLS, Columns: cols})
		}
		tableRows.Close()
		if err := tableRows.Err(); err != nil {
			return fmt.Errorf("registry: load from db: table rows: %w", err)
		}

		var authProviders map[string]any
		if len(a.authProviders) > 0 {
			json.Unmarshal(a.authProviders, &authProviders)
		}

		var storageCfg *config.StorageConfig
		if len(a.storageConfig) > 0 && string(a.storageConfig) != "{}" {
			var sc config.StorageConfig
			if err := json.Unmarshal(a.storageConfig, &sc); err == nil && sc.Bucket != "" {
				storageCfg = &sc
			}
		}

		var rateLimitCfg *config.RateLimitConfig
		if len(a.rateLimitConfig) > 0 && string(a.rateLimitConfig) != "{}" {
			var rc config.RateLimitConfig
			if err := json.Unmarshal(a.rateLimitConfig, &rc); err == nil && rc.Enabled {
				rateLimitCfg = &rc
			}
		}

		newApps[a.name] = &App{
			Config: config.AppConfig{
				Name: a.name,
				Auth: config.AuthConfig{
					JWTSecret: a.jwtSecret,
					Providers: config.AuthProviders{Email: a.authEmailEnabled},
				},
				Tables: tableCfgs,
			},
			SchemaName:    a.name,
			Tables:        tables,
			AuthProviders: authProviders,
			StorageConfig: storageCfg,
			RateLimit:     rateLimitCfg,
		}
	}

	r.mu.Lock()
	r.apps = newApps
	r.mu.Unlock()

	return nil
}

// Register adds or replaces a single app in the registry.
func (r *Registry) Register(app *App) {
	r.mu.Lock()
	r.apps[app.Config.Name] = app
	r.mu.Unlock()
}

// Unregister removes an app by name.
func (r *Registry) Unregister(appName string) {
	r.mu.Lock()
	delete(r.apps, appName)
	r.mu.Unlock()
}
