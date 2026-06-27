package registry

import (
	"fmt"
	"sync"

	"github.com/zeeplabs/zeep-core/internal/config"
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
	Columns []Column
}

// Column representa uma coluna de uma tabela.
type Column struct {
	Name     string
	Type     string
	Required bool
	Default  string
	Unique   bool
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
				})
			}
			tables[tblCfg.Name] = &Table{
				Name:    tblCfg.Name,
				Columns: cols,
			}
		}

		newApps[appCfg.Name] = &App{
			Config:     appCfg,
			SchemaName: "app_" + appCfg.Name,
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
