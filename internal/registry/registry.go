package registry

import (
	"sort"
	"sync"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// All read and write operations are protected by RWMutex.
type Registry struct {
	mu       sync.RWMutex
	apps     map[string]*App
	sysCfg   SystemConfig
	missHook func()
	// gen counts local writes (Register, Unregister, SetSystemConfig), so a
	// sync whose snapshot was read before one of them can tell it is stale.
	gen uint64
}

// Snapshot is the full registry state loaded from the database: every app
// plus the global system config, swapped in together by Replace.
type Snapshot struct {
	Apps   map[string]*App
	System SystemConfig
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

type Table struct {
	Name    string
	RLS     string
	Columns []Column
}

type Column struct {
	Name          string
	Type          string
	Required      bool
	Default       string
	Unique        bool
	RenameFrom    string
	AllowedValues []string
}

// SystemConfig holds global platform settings.
type SystemConfig struct {
	SoftDeleteEnabled  bool `json:"soft_delete_enabled"`
	StatementTimeoutMs int  `json:"statement_timeout_ms"`
}

func New() *Registry {
	return &Registry{
		apps: make(map[string]*App),
	}
}

// O segundo valor indica se o app foi encontrado.
// On a miss, the miss hook (if set) runs and the lookup is retried once, so
// an app created on another replica is found without waiting for the next
// poll.
func (r *Registry) Get(appName string) (*App, bool) {
	if app, ok := r.lookupApp(appName); ok {
		return app, true
	}
	if !r.runMissHook() {
		return nil, false
	}
	return r.lookupApp(appName)
}

// Returns (nil, false) if the app or table does not exist. A miss on either
// runs the miss hook and retries once, like Get.
func (r *Registry) GetTable(appName, tableName string) (*Table, bool) {
	if tbl, ok := r.lookupTable(appName, tableName); ok {
		return tbl, true
	}
	if !r.runMissHook() {
		return nil, false
	}
	return r.lookupTable(appName, tableName)
}

func (r *Registry) lookupApp(appName string) (*App, bool) {
	r.mu.RLock()
	app, ok := r.apps[appName]
	r.mu.RUnlock()
	return app, ok
}

func (r *Registry) lookupTable(appName, tableName string) (*Table, bool) {
	app, ok := r.lookupApp(appName)
	if !ok {
		return nil, false
	}
	tbl, ok := app.Tables[tableName]
	return tbl, ok
}

// runMissHook calls the miss hook outside the lock (it may call Replace)
// and reports whether one was set.
func (r *Registry) runMissHook() bool {
	r.mu.RLock()
	hook := r.missHook
	r.mu.RUnlock()
	if hook == nil {
		return false
	}
	hook()
	return true
}

// SetMissHook sets the function Get and GetTable call on a miss before
// retrying the lookup once. The hook must rate-limit itself: it runs on the
// request path. nil disables it.
func (r *Registry) SetMissHook(fn func()) {
	r.mu.Lock()
	r.missHook = fn
	r.mu.Unlock()
}

// Replace swaps every app and the system config in one step, so a reader
// never sees apps from one load next to the system config of another. The
// registry takes ownership of apps.
func (r *Registry) Replace(apps map[string]*App, sys SystemConfig) {
	if apps == nil {
		apps = make(map[string]*App)
	}
	r.mu.Lock()
	r.apps = apps
	r.sysCfg = sys
	r.mu.Unlock()
}

// Generation returns the local write counter. Pair it with
// ReplaceIfGeneration.
func (r *Registry) Generation() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gen
}

// ReplaceIfGeneration is Replace, but only if no local write happened since
// Generation returned gen. It reports whether it replaced. A sync loads its
// snapshot from the database while the pod keeps serving: if this pod
// registered a change it just committed during that load, the snapshot may
// predate the commit, and swapping it in would roll the pod back until the
// next poll.
func (r *Registry) ReplaceIfGeneration(gen uint64, apps map[string]*App, sys SystemConfig) bool {
	if apps == nil {
		apps = make(map[string]*App)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gen != gen {
		return false
	}
	r.apps = apps
	r.sysCfg = sys
	return true
}

// Returns a copy of the slice without exposing the internal map.
func (r *Registry) Apps() []*App {
	r.mu.RLock()
	result := make([]*App, 0, len(r.apps))
	for _, app := range r.apps {
		result = append(result, app)
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Config.Name < result[j].Config.Name })
	return result
}

// Register adds or replaces a single app in the registry.
func (r *Registry) Register(app *App) {
	r.mu.Lock()
	r.apps[app.Config.Name] = app
	r.gen++
	r.mu.Unlock()
}

// Unregister removes an app by name.
func (r *Registry) Unregister(appName string) {
	r.mu.Lock()
	delete(r.apps, appName)
	r.gen++
	r.mu.Unlock()
}

// SetSystemConfig stores the global system config.
func (r *Registry) SetSystemConfig(cfg SystemConfig) {
	r.mu.Lock()
	r.sysCfg = cfg
	r.gen++
	r.mu.Unlock()
}

// SystemConfig returns the current global system config.
func (r *Registry) SystemConfig() SystemConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sysCfg
}
