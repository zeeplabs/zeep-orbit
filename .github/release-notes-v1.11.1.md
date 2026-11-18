## v1.11.1

Patch release. Each app can now choose the role new end users get on their first sign-in, instead of the fixed `member`.

### Added

- **Default end-user role per app.** Pick it in the app's **End-user roles** panel (button "Set as default" on a role), or send `default_role` to `PUT /dashboard/api/apps/{id}/roles`. A request with only `default_role` leaves the role list untouched. New email and Google sign-ups receive it. The default role can't be removed from the list until another one is chosen (`409 role is default`).
- The default is stored in the `DEFAULT` of the app's `_auth_users.role` column and in `zeep_system.apps.default_enduser_role`, both changed in one transaction, so every replica applies it at once without a restart.

### Upgrade notes

- **Nothing changes for existing apps:** the default stays `member`. The migration is additive (`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`) and runs at boot. Pods on the previous version keep working during a rolling deploy.
- **If you ever set the default of `_auth_users.role` by hand in an app, the panel will show `member` until you pick the real default there.** Setting it in the panel repeats the change and aligns both. To list apps whose database default is not `member`:

  ```sql
  SELECT table_schema, column_default FROM information_schema.columns
  WHERE table_name = '_auth_users' AND column_name = 'role'
    AND column_default IS DISTINCT FROM '''member''::text';
  ```

- Changing the default briefly takes a lock on the app's `_auth_users` table (waits at most 5s and answers `409` on timeout).
- Users who already signed up keep their role; only new sign-ups get the new default.
- Setting a default on an app without end-user auth answers `400 end-user auth is not enabled for this app`.
