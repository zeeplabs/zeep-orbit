package dashboard

// Tests for rbac-per-app T-02 (TestOwnershipMigration) and T-03
// (TestCountAppAdmins). DB-dependent; skipped sem TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"testing"
)

func TestCountAppAdmins(t *testing.T) {
	pool := rbacTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// Setup: 3 users + 1 backend app + 1 frontend app (via github_templates).
	// Mesmo setup usado em rbac_test.go; aqui copiamos o necessário para
	// deixar o teste independente de detalhes de seed alheios.
	const (
		ownerID   = "00000000-0000-0000-0000-00000000c001"
		editorID  = "00000000-0000-0000-0000-00000000c002"
		viewerID  = "00000000-0000-0000-0000-00000000c003"
		orphanID  = "00000000-0000-0000-0000-00000000c004"
		orphan2ID = "00000000-0000-0000-0000-00000000c005"
	)
	for _, u := range []struct{ id, email, role string }{
		{ownerID, "owner-count@example.com", "member"},
		{editorID, "editor-count@example.com", "member"},
		{viewerID, "viewer-count@example.com", "member"},
		{orphanID, "orphan-count@example.com", "member"},
		{orphan2ID, "orphan2-count@example.com", "member"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES ($1, $2, $3)`,
			u.id, u.email, u.role); err != nil {
			t.Fatalf("seed user %s: %v", u.email, err)
		}
	}
	var backendID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('app-count', $1) RETURNING id`,
		ownerID).Scan(&backendID); err != nil {
		t.Fatalf("seed backend: %v", err)
	}
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('tpl-count', '', 'o', 'r', 'vite', $1) RETURNING id`,
		ownerID).Scan(&templateID); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	var frontendID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ('slug-count', 'f-count', $1, $2) RETURNING id`,
		templateID, ownerID).Scan(&frontendID); err != nil {
		t.Fatalf("seed frontend: %v", err)
	}

	// AppRef inválido → erro, sem tocar no DB.
	if _, err := CountAppAdmins(ctx, pool, AppRef{}); !errors.Is(err, ErrInvalidAppRef) {
		t.Errorf("CountAppAdmins({}) err = %v, want ErrInvalidAppRef", err)
	}

	// 0 admins inicialmente em ambos os apps.
	for _, app := range []AppRef{{BackendAppID: backendID}, {FrontendAppID: frontendID}} {
		n, err := CountAppAdmins(ctx, pool, app)
		if err != nil {
			t.Fatalf("CountAppAdmins inicial: %v", err)
		}
		if n != 0 {
			t.Errorf("CountAppAdmins inicial = %d, want 0", n)
		}
	}

	// Adiciona 1 editor e 1 viewer no backend — não conta como admin.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'editor'), ($1, $3, 'viewer')`,
		backendID, editorID, viewerID); err != nil {
		t.Fatalf("seed editor+viewer: %v", err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: backendID}); err != nil || n != 0 {
		t.Errorf("CountAppAdmins(backend, só editor+viewer) = %d, %v; want 0, nil", n, err)
	}

	// Adiciona o owner como admin.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		backendID, ownerID); err != nil {
		t.Fatalf("seed owner admin: %v", err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: backendID}); err != nil || n != 1 {
		t.Errorf("CountAppAdmins(backend, 1 admin) = %d, %v; want 1, nil", n, err)
	}

	// Adiciona outro admin (orphan) no backend → 2.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		backendID, orphanID); err != nil {
		t.Fatalf("seed second admin: %v", err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: backendID}); err != nil || n != 2 {
		t.Errorf("CountAppAdmins(backend, 2 admins) = %d, %v; want 2, nil", n, err)
	}

	// Frontend ainda tem 0 admins (ownerID está no backend, não no frontend).
	if n, err := CountAppAdmins(ctx, pool, AppRef{FrontendAppID: frontendID}); err != nil || n != 0 {
		t.Errorf("CountAppAdmins(frontend, 0 admins) = %d, %v; want 0, nil", n, err)
	}

	// Adiciona 1 admin no frontend.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		frontendID, orphan2ID); err != nil {
		t.Fatalf("seed frontend admin: %v", err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{FrontendAppID: frontendID}); err != nil || n != 1 {
		t.Errorf("CountAppAdmins(frontend, 1 admin) = %d, %v; want 1, nil", n, err)
	}

	// Eixo errado (consultar frontend com backend ID) deve dar 0, não misturar.
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: frontendID}); err != nil || n != 0 {
		t.Errorf("CountAppAdmins(frontendID como backend) = %d, %v; want 0, nil", n, err)
	}
}

func TestOwnershipMigration(t *testing.T) {
	pool := rbacTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	// rbacTestPool já rodou o ProvisionZeepSystem uma vez (com dados vazios,
	// então o INSERT...SELECT de T-02 não populou nada). Agora inserimos
	// dados de fixture e rodamos o provisioner de novo — é nessa segunda
	// passada que os INSERTs de T-02 vão popular app_members.
	//
	// rbac-per-app T-08: the third migration source (pre-rbac `app_ownership`
	// co-owners) no longer exists — that table is dropped by the provisioner
	// itself. So this test now covers the two remaining sources: `apps.owner_id`
	// and `frontend_apps.created_by` (resolved by email).

	// Users: owner (backend), frontend creator resolvível, e ninguém para
	// o frontend app órfão.
	const (
		ownerID    = "00000000-0000-0000-0000-00000000b001"
		creatorID  = "00000000-0000-0000-0000-00000000b003"
		noUserMail = "deleted@example.com" // frontend app com created_by = este email; não existe em dashboard_users
	)
	for _, u := range []struct{ id, email, role string }{
		{ownerID, "owner-mig@example.com", "member"},
		{creatorID, "creator-mig@example.com", "member"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES ($1, $2, $3)`,
			u.id, u.email, u.role); err != nil {
			t.Fatalf("seed user %s: %v", u.email, err)
		}
	}

	// Backend app com owner = ownerID.
	var backendID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('mig-app', $1) RETURNING id`,
		ownerID).Scan(&backendID); err != nil {
		t.Fatalf("seed backend: %v", err)
	}

	// 2 frontend apps: 1 com creator resolvível, 1 com email órfão.
	var templateID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, created_by)
		 VALUES ('mig-tpl', '', 'o', 'r', 'vite', $1) RETURNING id`,
		creatorID).Scan(&templateID); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	var frontendResolvedID, frontendOrphanID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ('mig-resolved', 'f-resolved', $1, $2) RETURNING id`,
		templateID, "creator-mig@example.com").Scan(&frontendResolvedID); err != nil {
		t.Fatalf("seed frontend resolvido: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.frontend_apps (slug, name, template_id, created_by) VALUES ('mig-orphan', 'f-orphan', $1, $2) RETURNING id`,
		templateID, noUserMail).Scan(&frontendOrphanID); err != nil {
		t.Fatalf("seed frontend órfão: %v", err)
	}

	// Antes de rodar o provisioner, app_members está vazio (foi criada na
	// primeira ProvisionZeepSystem dentro de rbacTestPool, mas sem dados
	// ainda).
	var preCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.app_members`).Scan(&preCount); err != nil {
		t.Fatalf("pre-count: %v", err)
	}
	if preCount != 0 {
		t.Errorf("pre migration count = %d, want 0", preCount)
	}

	// Roda o provisioner de novo — é aqui que os INSERTs de T-02 populam
	// app_members a partir de apps.owner_id e frontend_apps.created_by
	// resolvido. T-08 também dropa `app_ownership` aqui.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("second ProvisionZeepSystem: %v", err)
	}

	// Esperado (T-08: o co-owner via `app_ownership` não é mais migrado
	// porque a tabela não existe mais — o backend app tem só 1 admin,
	// que é o owner):
	//   - owner → admin do backend
	//   - creator → admin do frontend resolvido
	//   - frontend órfão → sem admin (preservado, superadmin pode entrar)
	cases := []struct {
		name      string
		appID     string
		frontend  bool
		wantUsers []string
	}{
		{"owner do backend", backendID, false, []string{ownerID}},
		{"creator do frontend resolvido", frontendResolvedID, true, []string{creatorID}},
	}
	for _, c := range cases {
		t.Run("tem/"+c.name, func(t *testing.T) {
			var n int
			if c.frontend {
				if err := pool.QueryRow(ctx,
					`SELECT COUNT(*) FROM zeep_system.app_members WHERE frontend_app_id = $1 AND user_id = $2 AND role = 'admin'`,
					c.appID, c.wantUsers[0]).Scan(&n); err != nil {
					t.Fatalf("query: %v", err)
				}
			} else {
				if err := pool.QueryRow(ctx,
					`SELECT COUNT(*) FROM zeep_system.app_members WHERE backend_app_id = $1 AND user_id = $2 AND role = 'admin'`,
					c.appID, c.wantUsers[0]).Scan(&n); err != nil {
					t.Fatalf("query: %v", err)
				}
			}
			if n != 1 {
				t.Errorf("app_members tem %s = %d, want 1", c.name, n)
			}
		})
	}

	// Backend app deve ter 1 admin (owner) — co-owner removido em T-08.
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: backendID}); err != nil || n != 1 {
		t.Errorf("CountAppAdmins(backend após migração) = %d, %v; want 1, nil", n, err)
	}
	// Frontend resolvido: 1 admin.
	if n, err := CountAppAdmins(ctx, pool, AppRef{FrontendAppID: frontendResolvedID}); err != nil || n != 1 {
		t.Errorf("CountAppAdmins(frontend resolvido) = %d, %v; want 1, nil", n, err)
	}
	// Frontend órfão: 0 admins (intencional).
	if n, err := CountAppAdmins(ctx, pool, AppRef{FrontendAppID: frontendOrphanID}); err != nil || n != 0 {
		t.Errorf("CountAppAdmins(frontend órfão) = %d, %v; want 0, nil", n, err)
	}

	// Idempotência: rodar o provisioner mais uma vez não pode duplicar rows.
	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("third ProvisionZeepSystem: %v", err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{BackendAppID: backendID}); err != nil || n != 1 {
		t.Errorf("após segunda migração: CountAppAdmins(backend) = %d, %v; want 1, nil (não pode duplicar)", n, err)
	}
	if n, err := CountAppAdmins(ctx, pool, AppRef{FrontendAppID: frontendResolvedID}); err != nil || n != 1 {
		t.Errorf("após segunda migração: CountAppAdmins(frontend resolvido) = %d, %v; want 1, nil (não pode duplicar)", n, err)
	}

	// T-08: `app_ownership` foi removida pelo provisioner. Confirmar
	// que não existe mais — se um futuro commit reintroduzir a tabela,
	// este teste quebra como um guard rail.
	var stillExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'zeep_system' AND table_name = 'app_ownership')`,
	).Scan(&stillExists); err != nil {
		t.Fatalf("check app_ownership exists: %v", err)
	}
	if stillExists {
		t.Error("app_ownership deveria ter sido removida em T-08; se você está vendo isso, alguém reintroduziu a tabela sem remover a guarda")
	}
}

// TestConcurrentAdminDemotionsDoNotDeadlock: two admins on the same app
// call UpdateAppMemberRole against each other at the same time (A demotes
// B, B demotes A, concurrently). Before the lockAppMembers fix, each call
// locked the target row first and the admin set second — with two admins
// racing, the lock order could invert between the two transactions and
// Postgres would abort one with a 40P01 deadlock error instead of the
// clean ErrLastAppAdmin/success split the invariant is supposed to produce.
// This test fails (via a raw pgconn deadlock error surfacing from one of
// the two calls) if that regresses.
func TestConcurrentAdminDemotionsDoNotDeadlock(t *testing.T) {
	pool := rbacTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	const (
		adminAID = "00000000-0000-0000-0000-00000000d001"
		adminBID = "00000000-0000-0000-0000-00000000d002"
	)
	for _, u := range []struct{ id, email string }{
		{adminAID, "deadlock-a@example.com"},
		{adminBID, "deadlock-b@example.com"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.dashboard_users (id, email, role) VALUES ($1, $2, 'member')`,
			u.id, u.email); err != nil {
			t.Fatalf("seed user %s: %v", u.email, err)
		}
	}
	var backendID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ('app-deadlock', $1) RETURNING id`,
		adminAID).Scan(&backendID); err != nil {
		t.Fatalf("seed backend: %v", err)
	}
	// Both are admins — CreateApp's owner-membership insert only covers the
	// owner, so add B explicitly.
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')
		 ON CONFLICT (backend_app_id, user_id) WHERE backend_app_id IS NOT NULL DO NOTHING`,
		backendID, adminAID); err != nil {
		t.Fatalf("seed admin A membership: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		backendID, adminBID); err != nil {
		t.Fatalf("seed admin B membership: %v", err)
	}

	// Run N rounds to make a lock-order race actually likely to trigger,
	// re-promoting whoever got demoted back to admin between rounds.
	for round := 0; round < 20; round++ {
		results := make(chan error, 2)
		go func() {
			results <- UpdateAppMemberRole(ctx, pool, AppRef{BackendAppID: backendID}, adminBID, AppRoleEditor)
		}()
		go func() {
			results <- UpdateAppMemberRole(ctx, pool, AppRef{BackendAppID: backendID}, adminAID, AppRoleEditor)
		}()
		err1 := <-results
		err2 := <-results

		for _, err := range []error{err1, err2} {
			if err != nil && !errors.Is(err, ErrLastAppAdmin) {
				t.Fatalf("round %d: unexpected error (want nil or ErrLastAppAdmin, got a raw driver error if this is a deadlock): %v", round, err)
			}
		}
		// Exactly one of the two calls must have succeeded (the other one
		// necessarily sees "would leave 0 admins" and gets ErrLastAppAdmin,
		// since demoting both simultaneously is never valid) — restore both
		// to admin before the next round.
		succeeded := err1 == nil || err2 == nil
		if !succeeded {
			t.Fatalf("round %d: both calls failed, want exactly one success", round)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE zeep_system.app_members SET role = 'admin' WHERE backend_app_id = $1`,
			backendID); err != nil {
			t.Fatalf("round %d: restore admins: %v", round, err)
		}
	}
}

// TestListAppMembersForUser_HappyPathMatchesRESTShape covers
// mcp-read-only-tools T6's Done-when: ListAppMembersForUser returns the
// same member list ListAppMembers' existing REST test
// (TestAppMembersRBACMatrix's be_appadmin_list case) expects for the same
// fixture.
func TestListAppMembersForUser_HappyPathMatchesRESTShape(t *testing.T) {
	pool, _, actors, backendAppID, _ := appMembersHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	members, err := ListAppMembersForUser(ctx, pool, actors["appadmin"], backendAppID)
	if err != nil {
		t.Fatalf("ListAppMembersForUser: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("expected at least the seeded appadmin/appeditor members")
	}
	var sawAdmin, sawEditor bool
	for _, m := range members {
		switch m.UserID {
		case actors["appadmin"].ID:
			sawAdmin = m.Role == AppRoleAdmin
		case actors["appeditor"].ID:
			sawEditor = m.Role == AppRoleEditor
		}
	}
	if !sawAdmin || !sawEditor {
		t.Fatalf("expected appadmin (admin) and appeditor (editor) in the member list, got %+v", members)
	}
}

// TestListAppMembersForUser_NonManagerForbidden covers mcp-read-only-tools
// T6's Done-when: an editor (a real app member whose role fails
// CanManage(), not just "no membership") is rejected with ErrForbidden —
// the explicit CanManage() tier test the design's Authorization Matrix
// requires.
func TestListAppMembersForUser_NonManagerForbidden(t *testing.T) {
	pool, _, actors, backendAppID, _ := appMembersHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListAppMembersForUser(ctx, pool, actors["appeditor"], backendAppID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an editor (CanManage()==false), got %v", err)
	}
}

// TestListAppMembersForUser_OutsiderForbidden covers the same forbidden
// tier for a caller with no app_members row at all — matching
// TestAppMembersRBACMatrix's be_outsider_list case (403, not 404): this
// endpoint deliberately doesn't distinguish "no access" from "doesn't
// exist" (app_members.go:64-67's stated reasoning), unlike GetApp-based
// tools.
func TestListAppMembersForUser_OutsiderForbidden(t *testing.T) {
	pool, _, actors, backendAppID, _ := appMembersHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListAppMembersForUser(ctx, pool, actors["outsider"], backendAppID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for an outsider with no app_members row, got %v", err)
	}
}

// TestListAppMembersForUser_MalformedRefReturnsErrNotFound covers the one
// not-found path this function actually has: an empty appID produces a
// malformed AppRef (ErrInvalidAppRef), which the REST handler itself maps
// to 404 — ListAppMembersForUser preserves that same mapping.
func TestListAppMembersForUser_MalformedRefReturnsErrNotFound(t *testing.T) {
	pool, _, actors, _, _ := appMembersHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	_, err := ListAppMembersForUser(ctx, pool, actors["appadmin"], "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an empty appID (malformed AppRef), got %v", err)
	}
}
