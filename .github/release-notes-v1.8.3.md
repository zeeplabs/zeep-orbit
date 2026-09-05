## v1.8.3

Patch release fixing a Google sign-in provisioning bug and several email-case-sensitivity bugs across Google and email/password login.

### Fixed

- **Auth tables never provisioned for Google-only apps.** An app configured with Google as its only auth provider (email/password never toggled on) got no `_auth_users`/`_auth_sessions` tables provisioned, so every Google sign-in attempt failed with a raw database error. `EnsureAuthTables` provisioning was previously gated solely on the email/password flag; it now runs whenever any auth provider is enabled — fixed across every place an app's auth config is created or updated: the dashboard's app-update form, the dedicated auth-providers endpoint, app creation, and the MCP/AI-edit update path.
- **Google sign-in email-case mismatch.** Both the per-app Google login and the platform dashboard's own Google login compared the account's email case-sensitively. A user whose Google account's email casing didn't exactly match their existing stored email could end up with a duplicate account — on the dashboard, a second account created with **admin** role instead of being linked to their existing one. Both paths now lowercase the email before lookup, matching how registration already normalizes it.
- **Email/password login didn't normalize the submitted email.** Unlike registration, the end-user app login endpoint compared the submitted email as-is, so a correctly-registered user could fail to log in just by typing their email with different casing.

### Upgrade notes

No manual migration needed — the auth-table provisioning gap self-heals the next time an affected app's auth config is saved (`EnsureAuthTables` is idempotent). No breaking changes.
