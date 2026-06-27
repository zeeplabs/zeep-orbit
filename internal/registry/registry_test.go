package registry

import (
	"sync"
	"testing"

	"github.com/zeep-tecnologia/zeep-core/internal/config"
)

// buildConfig é um helper que monta um config.Config com N apps simples.
func buildConfig(apps ...config.AppConfig) *config.Config {
	return &config.Config{Apps: apps}
}

func sampleApp(name string) config.AppConfig {
	return config.AppConfig{
		Name: name,
		Auth: config.AuthConfig{JWTSecret: "secret-" + name},
		Tables: []config.TableConfig{
			{
				Name: "users",
				Columns: []config.ColumnConfig{
					{Name: "id", Type: "uuid", Required: true, Unique: true},
					{Name: "email", Type: "text", Required: true, Unique: true},
					{Name: "role", Type: "text", Default: "viewer"},
				},
			},
			{
				Name: "posts",
				Columns: []config.ColumnConfig{
					{Name: "id", Type: "uuid", Required: true},
					{Name: "title", Type: "text", Required: true},
				},
			},
		},
	}
}

func TestLoad(t *testing.T) {
	r := New()
	cfg := buildConfig(sampleApp("alpha"), sampleApp("beta"), sampleApp("gamma"))

	if err := r.Load(cfg); err != nil {
		t.Fatalf("Load inesperado: %v", err)
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
		want := "app_" + name
		if app.SchemaName != want {
			t.Errorf("app.SchemaName: esperado %q, obteve %q", want, app.SchemaName)
		}
		if len(app.Tables) != 2 {
			t.Errorf("app %q: esperado 2 tabelas, obteve %d", name, len(app.Tables))
		}
	}
}

func TestGetMissing(t *testing.T) {
	r := New()
	cfg := buildConfig(sampleApp("only"))
	if err := r.Load(cfg); err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get(\"nonexistent\"): esperado false, obteve true")
	}
}

func TestGetTable(t *testing.T) {
	r := New()
	if err := r.Load(buildConfig(sampleApp("myapp"))); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// tabela existente
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

	// tabela inexistente dentro de app existente
	_, ok = r.GetTable("myapp", "nope")
	if ok {
		t.Error("GetTable(\"myapp\", \"nope\"): esperado false, obteve true")
	}

	// app inexistente
	_, ok = r.GetTable("ghost", "users")
	if ok {
		t.Error("GetTable(\"ghost\", \"users\"): esperado false, obteve true")
	}
}

func TestConcurrentReads(t *testing.T) {
	r := New()
	cfg := buildConfig(sampleApp("concurrent"))
	if err := r.Load(cfg); err != nil {
		t.Fatalf("Load: %v", err)
	}

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

func TestLoadReplace(t *testing.T) {
	r := New()

	// primeira carga: app "first"
	if err := r.Load(buildConfig(sampleApp("first"))); err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	if _, ok := r.Get("first"); !ok {
		t.Fatal("após Load 1: \"first\" deveria existir")
	}

	// segunda carga: somente app "second" — deve substituir o estado anterior
	if err := r.Load(buildConfig(sampleApp("second"))); err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	if _, ok := r.Get("first"); ok {
		t.Error("após Load 2: \"first\" não deveria mais existir")
	}
	if _, ok := r.Get("second"); !ok {
		t.Error("após Load 2: \"second\" deveria existir")
	}

	apps := r.Apps()
	if len(apps) != 1 {
		t.Errorf("Apps() após Load 2: esperado 1, obteve %d", len(apps))
	}
}
