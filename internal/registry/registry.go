package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Registry mantém o mapa de apps carregados em memória.
// Todas as operações de leitura e escrita são protegidas por RWMutex.
type Registry struct {
	mu   sync.RWMutex
	apps map[string]*App
}

// App representa uma aplicação com seu esquema e tabelas.
type App struct {
	Config     config.AppConfig
	SchemaName string // "app_{name}"
	Tables     map[string]*Table
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

// Load popula o registry a partir de cfg.
// Substitui completamente o estado anterior (re-load seguro).
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

// Get retorna o App pelo nome (case-sensitive).
// O segundo valor indica se o app foi encontrado.
func (r *Registry) Get(appName string) (*App, bool) {
	r.mu.RLock()
	app, ok := r.apps[appName]
	r.mu.RUnlock()
	return app, ok
}

// GetTable retorna a Table dentro de um App.
// Retorna (nil, false) se o app ou a tabela não existir.
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

// Apps retorna uma lista com todos os apps carregados.
// Retorna uma cópia da slice sem expor o mapa interno.
func (r *Registry) Apps() []*App {
	r.mu.RLock()
	result := make([]*App, 0, len(r.apps))
	for _, app := range r.apps {
		result = append(result, app)
	}
	r.mu.RUnlock()
	return result
}

// LoadFromDB populates the registry from zeep_system DB tables.
// Replaces any existing state. Safe to call on startup.
func (r *Registry) LoadFromDB(ctx context.Context, pool *db.Pool) error {
	type appRow struct {
		id               string
		name             string
		jwtSecret        string
		authEmailEnabled bool
	}

	rows, err := pool.Query(ctx,
		`SELECT id, name, jwt_secret, auth_email_enabled FROM zeep_system.apps ORDER BY name`,
	)
	if err != nil {
		return fmt.Errorf("registry: load from db: query apps: %w", err)
	}
	defer rows.Close()

	var appRows []appRow
	for rows.Next() {
		var a appRow
		if err := rows.Scan(&a.id, &a.name, &a.jwtSecret, &a.authEmailEnabled); err != nil {
			return fmt.Errorf("registry: load from db: scan app: %w", err)
		}
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

		newApps[a.name] = &App{
			Config: config.AppConfig{
				Name: a.name,
				Auth: config.AuthConfig{
					JWTSecret: a.jwtSecret,
					Providers: config.AuthProviders{Email: a.authEmailEnabled},
				},
				Tables: tableCfgs,
			},
			SchemaName: a.name,
			Tables:     tables,
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
