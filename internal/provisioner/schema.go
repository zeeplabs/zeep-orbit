package provisioner

import (
	"context"
	"fmt"
)

// enduserRole is the Postgres role the server SET LOCAL ROLEs into for
// end-user requests (bootstrapped by dashboard.ProvisionZeepSystem — see
// end-user-row-policies T3). It has no ownership and no BYPASSRLS, so it is
// the one that actually observes native RLS policies.
const enduserRole = "zeep_app_enduser"

// (did not exist before), false if it already existed.
func (p *Provisioner) createSchema(ctx context.Context, schemaName string) (bool, error) {
	// Checks existence first to detect if it was just created.
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)`,
		schemaName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("schema: check existence %q: %w", schemaName, err)
	}

	if exists {
		if err := p.grantEnduserSchemaAccess(ctx, schemaName); err != nil {
			return false, err
		}
		return false, nil
	}

	_, err = p.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schemaName))
	if err != nil {
		return false, fmt.Errorf("schema: create %q: %w", schemaName, err)
	}

	if err := p.grantEnduserSchemaAccess(ctx, schemaName); err != nil {
		return false, err
	}

	return true, nil
}

// BackfillEnduserGrants re-runs grantEnduserSchemaAccess for every schema
// name given. Apply/createSchema only grants access to a schema when an app
// is created or edited through the Dashboard — an app that hasn't been
// touched since end-user-row-policies shipped never gets the GRANT, so its
// end-user requests (which now always run as zeep_app_enduser, not the
// owner role — see db.Pool.WithRLSContext) fail with "permission denied".
// Call this once at boot for every app already in the registry so upgrading
// doesn't silently break existing apps. Idempotent, safe to run every boot.
// Returns one error per schema that failed, so a single bad schema doesn't
// stop the rest from being granted (and doesn't need to block boot — the
// caller decides whether a partial failure is fatal).
func (p *Provisioner) BackfillEnduserGrants(ctx context.Context, schemaNames []string) []error {
	var errs []error
	for _, schemaName := range schemaNames {
		if err := p.grantEnduserSchemaAccess(ctx, schemaName); err != nil {
			errs = append(errs, fmt.Errorf("provisioner: backfill enduser grants %q: %w", schemaName, err))
		}
	}
	return errs
}

// grantEnduserSchemaAccess ensures zeep_app_enduser can access every table in
// schemaName — the ones that already exist (explicit GRANT on all current
// tables, covering apps that existed before this feature shipped) and the
// ones created in the future (ALTER DEFAULT PRIVILEGES, so new tables in an
// existing app, or in a brand-new app, never need a separate migration
// pass). Idempotent — GRANT/ALTER DEFAULT PRIVILEGES are safe to re-run.
//
// The role itself is normally bootstrapped once by
// dashboard.ProvisionZeepSystem (end-user-row-policies T3), which also runs
// before this package's Apply in the real server startup path. This method
// creates it too, defensively and idempotently, so internal/provisioner
// keeps working standalone (as its own tests do) regardless of that
// ordering.
func (p *Provisioner) grantEnduserSchemaAccess(ctx context.Context, schemaName string) error {
	stmts := []string{
		fmt.Sprintf(`DO $do$
		 BEGIN
		   IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN
		     CREATE ROLE %s NOSUPERUSER NOBYPASSRLS NOLOGIN;
		   END IF;
		 END
		 $do$`, enduserRole, enduserRole),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %q TO %s`, schemaName, enduserRole),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %q TO %s`, schemaName, enduserRole),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA %q GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s`, schemaName, enduserRole),
	}
	for _, stmt := range stmts {
		if _, err := p.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("schema: grant enduser access %q: %w", schemaName, err)
		}
	}
	return nil
}
