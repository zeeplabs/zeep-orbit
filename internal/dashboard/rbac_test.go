package dashboard

// Tests for rbac-per-app T-01: app_members table + ResolveAppRole.
//
// Puro (sem DB): TestAppRoleMethods, TestResolveAppRoleInvalidRef.
// DB-dependent: TestResolveAppRole (matrix), TestAppMembersSchemaConstraints
// (UNIQUE parcial, CHECK, ON DELETE CASCADE). Pulados quando TEST_DATABASE_URL
// não estiver setado — mesmo padrão de provisioner_roles_test.go.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// --- testes puros (sem DB) ---

func TestAppRoleMethods(t *testing.T) {
	cases := []struct {
		role      AppRole
		effective bool
		canWrite  bool
		canManage bool
	}{
		{"", false, false, false}, // zero value: not a member
		{"admin", true, true, true},
		{"editor", true, true, false},
		{"viewer", true, false, false},
		// Defensive: unknown role values — neither write nor manage.
		{"godmode", true, false, false},
	}
	for _, c := range cases {
		if got := c.role.Effective(); got != c.effective {
			t.Errorf("AppRole(%q).Effective() = %v, want %v", c.role, got, c.effective)
		}
		if got := c.role.CanWrite(); got != c.canWrite {
			t.Errorf("AppRole(%q).CanWrite() = %v, want %v", c.role, got, c.canWrite)
		}
		if got := c.role.CanManage(); got != c.canManage {
			t.Errorf("AppRole(%q).CanManage() = %v, want %v", c.role, got, c.canManage)
		}
	}
}

func TestResolveAppRoleInvalidRef(t *testing.T) {
	// Puro — só valida a forma do AppRef. Não toca em pool/user.
	pool := &db.Pool{} // zero value: nunca é usado se AppRef falhar primeiro
	user := &DashboardUser{ID: "u", Role: "member"}

	cases := []struct {
		name string
		ref  AppRef
	}{
		{"both empty", AppRef{}},
		{"both set", AppRef{BackendAppID: "b", FrontendAppID: "f"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			role, err := ResolveAppRole(context.Background(), pool, user, c.ref)
			if !errors.Is(err, ErrInvalidAppRef) {
				t.Errorf("err = %v, want ErrInvalidAppRef", err)
			}
			if role != "" {
				t.Errorf("role = %q, want empty", role)
			}
		})
	}

	// Nil user deve falhar também (defensivo — programmer error).
	t.Run("nil user", func(t *testing.T) {
		role, err := ResolveAppRole(context.Background(), pool, nil, AppRef{BackendAppID: "b"})
		if err == nil {
			t.Error("err = nil, want non-nil")
		}
		if role != "" {
			t.Errorf("role = %q, want empty", role)
		}
	})
}

// --- testes com DB real ---

// rbacTestPool conecta ao test DB, droa o schema inteiro e roda o
// ProvisionZeepSystem real. Isso garante que estamos testando contra o
// schema verdadeiro (app_members + indices + CHECK + todas as FKs) e não
// uma versão simplificada que poderia divergir.
func rbacTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS zeep_system CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("drop schema: %v", err)
	}

	// Roda o provisioner real — ele cria tudo: dashboard_users, apps,
	// app_ownership, app_members (com indices + CHECK), frontend_apps, etc.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ProvisionZeepSystem: %v", err)
	}

	return pool
}

func TestResolveAppRole(t *testing.T) {
	pool := rbacTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Seed: 5 dashboard users (1 por role global + 1 com membership explícita),
	// 1 backend app, 1 frontend app, e 1 row em app_members para o "alice_member"
	// ser admin do backend app + viewer do frontend app.
	users := []struct {
		id    string
		email string
		role  string
	}{
		{"00000000-0000-0000-0000-000000000001", "super@example.com", "superadmin"},
		{"00000000-0000-0000-0000-000000000002", "admin@example.com", "admin"},
		{"00000000-0000-0000-0000-000000000003", "auditor@example.com", "auditor"},
		{"00000000-0000-0000-0000-000000000004", "member@example.com", "member"},
		{"00000000-0000-0000-0000-000000000005", "alice@example.com", "member"},
	}
	for _, u := range users {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES ($1, $2, $3)`,
			u.id, u.email, u.role); err != nil {
			t.Fatalf("seed user %s: %v", u.email, err)
		}
	}

	var backendAppID, frontendAppID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"app1", users[3].id).Scan(&backendAppID); err != nil {
		t.Fatalf("seed backend app: %v", err)
	}
	// frontend_apps.template_id é NOT NULL → cria um template antes.
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('tpl1', '', 'owner', 'repo', 'vite', $1) RETURNING id`,
		users[3].email).Scan(&templateID); err != nil {
		t.Fatalf("seed github_templates: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		"slug1", "Frontend1", templateID, "alice@example.com").Scan(&frontendAppID); err != nil {
		t.Fatalf("seed frontend app: %v", err)
	}

	// Alice: admin do backend app, viewer do frontend app.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		backendAppID, users[4].id); err != nil {
		t.Fatalf("seed alice backend: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		frontendAppID, users[4].id); err != nil {
		t.Fatalf("seed alice frontend: %v", err)
	}

	// Member sem nenhuma membership em lugar nenhum.
	memberNoMembership := &DashboardUser{ID: users[3].id, Role: "member"}

	cases := []struct {
		name string
		user *DashboardUser
		app  AppRef
		want AppRole
	}{
		// superadmin: bypass em qualquer app, qualquer eixo.
		{"superadmin x backend", &DashboardUser{ID: users[0].id, Role: "superadmin"}, AppRef{BackendAppID: backendAppID}, AppRoleAdmin},
		{"superadmin x frontend", &DashboardUser{ID: users[0].id, Role: "superadmin"}, AppRef{FrontendAppID: frontendAppID}, AppRoleAdmin},

		// admin/auditor global: CanReadAnyApp true → viewer (cross-spec extension).
		{"admin global x backend (no membership)", &DashboardUser{ID: users[1].id, Role: "admin"}, AppRef{BackendAppID: backendAppID}, AppRoleViewer},
		{"admin global x frontend (no membership)", &DashboardUser{ID: users[1].id, Role: "admin"}, AppRef{FrontendAppID: frontendAppID}, AppRoleViewer},
		{"auditor global x backend (no membership)", &DashboardUser{ID: users[2].id, Role: "auditor"}, AppRef{BackendAppID: backendAppID}, AppRoleViewer},
		{"auditor global x frontend (no membership)", &DashboardUser{ID: users[2].id, Role: "auditor"}, AppRef{FrontendAppID: frontendAppID}, AppRoleViewer},

		// member sem membership: "" (sem acesso).
		{"member x backend (no membership)", memberNoMembership, AppRef{BackendAppID: backendAppID}, ""},
		{"member x frontend (no membership)", memberNoMembership, AppRef{FrontendAppID: frontendAppID}, ""},

		// alice tem row em app_members: retorna a role.
		{"alice member x backend (admin row)", &DashboardUser{ID: users[4].id, Role: "member"}, AppRef{BackendAppID: backendAppID}, AppRoleAdmin},
		{"alice member x frontend (viewer row)", &DashboardUser{ID: users[4].id, Role: "member"}, AppRef{FrontendAppID: frontendAppID}, AppRoleViewer},
		// alice não tem row no "outro" app — sem acesso.
		// (alice é admin do backend, mas se rodarmos ResolveAppRole para um app
		// diferente, o lookup retorna ErrNoRows → "").
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveAppRole(ctx, pool, c.user, c.app)
			if err != nil {
				t.Fatalf("ResolveAppRole: %v", err)
			}
			if got != c.want {
				t.Errorf("ResolveAppRole = %q, want %q", got, c.want)
			}
		})
	}

	// alice num backend app em que não é membro: "".
	otherBackend := "00000000-0000-0000-0000-00000000ffff"
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.apps (id, name, owner_id) VALUES ($1, $2, $3)`,
		otherBackend, "app2", users[3].id); err != nil {
		t.Fatalf("seed other backend: %v", err)
	}
	got, err := ResolveAppRole(ctx, pool, &DashboardUser{ID: users[4].id, Role: "member"}, AppRef{BackendAppID: otherBackend})
	if err != nil {
		t.Fatalf("ResolveAppRole alice/other: %v", err)
	}
	if got != "" {
		t.Errorf("alice em outro backend = %q, want empty", got)
	}
}

func TestAppMembersSchemaConstraints(t *testing.T) {
	pool := rbacTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Setup mínimo: 1 owner (do app) + 1 member (com membership que vai ser
	// cascade-deletada) + 1 backend app + 1 frontend app.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES
		 ('00000000-0000-0000-0000-00000000a001', 'owner@example.com', 'member'),
		 ('00000000-0000-0000-0000-00000000a002', 'mem@example.com',   'member')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	var backendID, frontendID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('a1', '00000000-0000-0000-0000-00000000a001') RETURNING id`,
	).Scan(&backendID); err != nil {
		t.Fatalf("seed backend: %v", err)
	}
	// frontend_apps.template_id é NOT NULL → cria um template antes.
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('tpl1', '', 'owner', 'repo', 'vite', 'owner@example.com') RETURNING id`,
	).Scan(&templateID); err != nil {
		t.Fatalf("seed github_templates: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ('s1', 'f1', $1, 'owner@example.com') RETURNING id`,
		templateID).Scan(&frontendID); err != nil {
		t.Fatalf("seed frontend: %v", err)
	}

	memberID := "00000000-0000-0000-0000-00000000a002"

	// CHECK de role rejeita valores fora de admin/editor/viewer.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'godmode')`,
		backendID, memberID); err == nil {
		t.Error("CHECK should reject role 'godmode', got nil")
	}

	// CHECK "exactly one" rejeita ambos NULL.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (user_id, role) VALUES ($1, 'admin')`,
		memberID); err == nil {
		t.Error("CHECK should reject (backend_app_id NULL, frontend_app_id NULL), got nil")
	}

	// CHECK "exactly one" rejeita ambos setados.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, frontend_app_id, user_id, role) VALUES ($1, $2, $3, 'admin')`,
		backendID, frontendID, memberID); err == nil {
		t.Error("CHECK should reject (both set), got nil")
	}

	// Insere uma row válida pra usar nos testes de UNIQUE.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		backendID, memberID); err != nil {
		t.Fatalf("insert valid row: %v", err)
	}

	// UNIQUE parcial: não pode inserir 2 rows com mesmo (backend_app_id, user_id).
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'editor')`,
		backendID, memberID)
	if err == nil {
		t.Error("UNIQUE should reject duplicate (backend_app_id, user_id), got nil")
	}

	// UNIQUE parcial é por eixo: user pode ser admin do backend e viewer do
	// frontend (mesma pessoa, eixos diferentes).
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		frontendID, memberID); err != nil {
		t.Errorf("cross-axis insert should succeed, got: %v", err)
	}

	// UNIQUE no eixo frontend: não pode duplicar.
	_, err = pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'editor')`,
		frontendID, memberID)
	if err == nil {
		t.Error("UNIQUE should reject duplicate (frontend_app_id, user_id), got nil")
	}

	// ON DELETE CASCADE: deletar o user deve remover suas rows em app_members
	// (sem mexer no owner do app, que é outro user — apps.owner_id FK).
	if _, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.dashboard_users WHERE id = $1`,
		memberID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.app_members`).Scan(&remaining); err != nil {
		t.Fatalf("count after cascade: %v", err)
	}
	if remaining != 0 {
		t.Errorf("ON DELETE CASCADE should remove all rows, got %d", remaining)
	}

	// Sanity check: pgx.ErrNoRows continua sendo o erro que ResolveAppRole
	// usa para distinguir "não é membro" de falha de query. (Documenta o
	// contrato que o resto do código assume.)
	_ = pgx.ErrNoRows
}
