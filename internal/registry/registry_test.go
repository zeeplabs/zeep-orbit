package registry

import (
	"sync"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

// newTestApp builds an *App the way LoadFromDB would for production data —
// the same shape Load() used to build before it was removed as dead code
// (D-243/item 6: no production caller, registry is populated from the
// database, not from a config.Config file — see LoadFromDB).
func newTestApp(name string) *App {
	return &App{
		Config: config.AppConfig{
			Name: name,
			Auth: config.AuthConfig{JWTSecret: "secret-" + name},
		},
		SchemaName: name,
		Tables: map[string]*Table{
			"users": {
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "uuid", Required: true, Unique: true},
					{Name: "email", Type: "text", Required: true, Unique: true},
					{Name: "role", Type: "text", Default: "viewer"},
				},
			},
			"posts": {
				Name: "posts",
				Columns: []Column{
					{Name: "id", Type: "uuid", Required: true},
					{Name: "title", Type: "text", Required: true},
				},
			},
		},
	}
}

func TestRegister(t *testing.T) {
	r := New()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		r.Register(newTestApp(name))
	}

	for _, name := range []string{"alpha", "beta", "gamma"} {
		app, ok := r.Get(name)
		if !ok {
			t.Errorf("Get(%q): esperado true, obteve false", name)
			continue
		}
		if app.Config.Name != name {
			t.Errorf("app.Config.Name: esperado %q, obteve %q", name, app.Config.Name)
		}
		if app.SchemaName != name {
			t.Errorf("app.SchemaName: esperado %q, obteve %q", name, app.SchemaName)
		}
		if len(app.Tables) != 2 {
			t.Errorf("app %q: esperado 2 tabelas, obteve %d", name, len(app.Tables))
		}
	}
}

func TestGetMissing(t *testing.T) {
	r := New()
	r.Register(newTestApp("only"))

	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get(\"nonexistent\"): esperado false, obteve true")
	}
}

func TestGetTable(t *testing.T) {
	r := New()
	r.Register(newTestApp("myapp"))

	tbl, ok := r.GetTable("myapp", "users")
	if !ok {
		t.Fatal("GetTable(\"myapp\", \"users\"): esperado true, obteve false")
	}
	if tbl.Name != "users" {
		t.Errorf("tbl.Name: esperado \"users\", obteve %q", tbl.Name)
	}
	if len(tbl.Columns) != 3 {
		t.Errorf("users: esperado 3 colunas, obteve %d", len(tbl.Columns))
	}

	_, ok = r.GetTable("myapp", "nope")
	if ok {
		t.Error("GetTable(\"myapp\", \"nope\"): esperado false, obteve true")
	}

	_, ok = r.GetTable("ghost", "users")
	if ok {
		t.Error("GetTable(\"ghost\", \"users\"): esperado false, obteve true")
	}
}

func TestConcurrentReads(t *testing.T) {
	r := New()
	r.Register(newTestApp("concurrent"))

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				app, ok := r.Get("concurrent")
				if !ok || app == nil {
					t.Errorf("Get retornou false em leitura concorrente")
					return
				}
				_ = r.Apps()
				_, _ = r.GetTable("concurrent", "users")
			}
		}()
	}

	wg.Wait()
}

func TestUnregister(t *testing.T) {
	r := New()

	r.Register(newTestApp("first"))
	if _, ok := r.Get("first"); !ok {
		t.Fatal("após Register: \"first\" deveria existir")
	}

	r.Register(newTestApp("second"))
	if _, ok := r.Get("first"); !ok {
		t.Fatal("Register de \"second\" não deveria afetar \"first\"")
	}

	r.Unregister("first")
	if _, ok := r.Get("first"); ok {
		t.Error("após Unregister: \"first\" não deveria mais existir")
	}
	if _, ok := r.Get("second"); !ok {
		t.Error("após Unregister(\"first\"): \"second\" deveria continuar existindo")
	}

	apps := r.Apps()
	if len(apps) != 1 {
		t.Errorf("Apps() após Unregister: esperado 1, obteve %d", len(apps))
	}
}
