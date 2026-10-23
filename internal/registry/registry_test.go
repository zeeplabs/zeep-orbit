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

func TestReplace(t *testing.T) {
	r := New()
	r.Register(newTestApp("old"))

	r.Replace(map[string]*App{"new": newTestApp("new")}, SystemConfig{SoftDeleteEnabled: true, StatementTimeoutMs: 1234})

	if _, ok := r.Get("old"); ok {
		t.Error("após Replace: \"old\" não deveria mais existir")
	}
	if _, ok := r.Get("new"); !ok {
		t.Error("após Replace: \"new\" deveria existir")
	}
	if got := r.SystemConfig(); !got.SoftDeleteEnabled || got.StatementTimeoutMs != 1234 {
		t.Errorf("após Replace: SystemConfig = %+v", got)
	}

	r.Replace(nil, SystemConfig{})
	if len(r.Apps()) != 0 {
		t.Error("Replace(nil) deveria deixar o registry vazio")
	}
	r.Register(newTestApp("after-nil"))
	if _, ok := r.Get("after-nil"); !ok {
		t.Error("Register após Replace(nil) deveria funcionar")
	}
}

// TestReplaceConcurrentReaders checks under -race that readers never see a
// mix of two generations: every Apps() call returns apps from one Replace.
func TestReplaceConcurrentReaders(t *testing.T) {
	r := New()
	gen := func(tag string) map[string]*App {
		m := map[string]*App{}
		for _, n := range []string{"a", "b", "c"} {
			app := newTestApp(n)
			app.SchemaName = tag
			m[n] = app
		}
		return m
	}
	r.Replace(gen("x"), SystemConfig{})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				apps := r.Apps()
				if len(apps) != 3 {
					t.Errorf("Apps() retornou %d apps, esperado 3", len(apps))
					return
				}
				for _, a := range apps[1:] {
					if a.SchemaName != apps[0].SchemaName {
						t.Errorf("Apps() misturou gerações: %q e %q", apps[0].SchemaName, a.SchemaName)
						return
					}
				}
				_, _ = r.Get("a")
				_ = r.SystemConfig()
			}
		}()
	}
	for i := 0; i < 500; i++ {
		if i%2 == 0 {
			r.Replace(gen("y"), SystemConfig{SoftDeleteEnabled: true})
		} else {
			r.Replace(gen("x"), SystemConfig{})
		}
	}
	close(stop)
	wg.Wait()
}

func TestMissHook(t *testing.T) {
	t.Run("hit never calls the hook", func(t *testing.T) {
		r := New()
		r.Register(newTestApp("hit"))
		calls := 0
		r.SetMissHook(func() { calls++ })
		r.Get("hit")
		r.GetTable("hit", "users")
		if calls != 0 {
			t.Errorf("hook chamado %d vezes em hit, esperado 0", calls)
		}
	})

	t.Run("miss calls the hook once per lookup", func(t *testing.T) {
		r := New()
		r.Register(newTestApp("present"))
		calls := 0
		r.SetMissHook(func() { calls++ })
		if _, ok := r.Get("ghost"); ok {
			t.Error("Get(\"ghost\") deveria falhar")
		}
		if calls != 1 {
			t.Errorf("Get: hook chamado %d vezes, esperado 1", calls)
		}
		if _, ok := r.GetTable("present", "ghost_table"); ok {
			t.Error("GetTable de tabela inexistente deveria falhar")
		}
		if calls != 2 {
			t.Errorf("GetTable: hook chamado %d vezes no total, esperado 2", calls)
		}
	})

	t.Run("hook can Replace and the retry finds the app", func(t *testing.T) {
		r := New()
		r.SetMissHook(func() {
			r.Replace(map[string]*App{"late": newTestApp("late")}, SystemConfig{})
		})
		if _, ok := r.Get("late"); !ok {
			t.Error("Get deveria achar o app carregado pelo hook")
		}
		if _, ok := r.GetTable("late", "posts"); !ok {
			t.Error("GetTable deveria achar a tabela carregada pelo hook")
		}
	})

	t.Run("no hook keeps the old miss behavior", func(t *testing.T) {
		r := New()
		if _, ok := r.Get("ghost"); ok {
			t.Error("Get sem hook deveria falhar")
		}
		r.SetMissHook(func() { t.Error("hook removido não deveria ser chamado") })
		r.SetMissHook(nil)
		if _, ok := r.GetTable("ghost", "users"); ok {
			t.Error("GetTable sem hook deveria falhar")
		}
	})
}
