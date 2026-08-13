## Highlights

- **Inbound webhooks** — apps can now receive events pushed by an external provider (e.g. Google Workspace) and write them straight into a table, no code required. A new webhook starts in capture mode (records the first real payload without writing), the owner maps captured fields to table columns per event type (insert/update/delete, with a match-key for update/delete), then activates it for real deliveries. Every call — success, unmapped, invalid token, duplicate, write failure — is logged for 30 days on the app's new "Webhooks" tab. Writes run under a dedicated `webhook` RLS role, so the target table needs an explicit policy granting it access. Provider verification handshakes (Slack-style `challenge`) are handled automatically.
- **New `rls: "policy"` mode** — until now, RLS always applied an automatic `owner_id = $sub` filter on top of any table policy, so a policy could only narrow visibility within a user's own rows, never widen it (e.g. "an admin sees every row"). `"policy"` mode removes that automatic filter entirely and delegates 100% of visibility/write permission to the table's own policies, fail-closed (zero policies denies all end-user access). `owner_id` is now nullable in this mode, since a webhook delivery has no end-user identity to populate it with. Existing tables are unaffected and can switch into this mode without recreation or data loss.
- **Policy Templates** — creating a row policy no longer starts with the technical Column/Operator/Claim/Logic form. Six named templates ("Only the owner sees/edits", "All signed-in users with a role can view", "Nobody edits, read-only", "Visible when a value matches", "Open read, write restricted to the owner", plus a non-actionable "Blocked by default" explainer) cover the common cases. The old form is still there behind an "Advanced mode" toggle, alongside a new Help drawer with worked examples.

## Also in this release

- The callback URL for a webhook is now always visible (not just once at creation), with the token stored reversibly encrypted instead of one-way hashed.
- The mapping editor warns when the target table's policies don't yet grant the `webhook` role every command the delivery path actually needs.
- The dashboard now polls while a webhook waits for its first sample, instead of requiring a manual reload.
- Destructive confirmations (RLS mode switch, delete table/policy/webhook, rotate token) now use a consistent in-app dialog instead of the browser's native `confirm()`.
- The table editor's relationships/indexes tutorials moved from a centered modal to a side drawer, rewritten in plain language for non-technical readers.

## Security fixes

- Two concurrent webhook deliveries with the same `event_id` (a provider retrying, the normal at-least-once case) could both write instead of the second being deduplicated. Now serialized with a Postgres advisory lock — held on a small dedicated connection pool, so it can't exhaust the main pool under concurrent load with distinct event ids.
- Deleting a webhook's event mapping wasn't scoped to its own webhook — any app could delete another app's mapping by guessing its id.
- The public webhook delivery route had no rate limit or body size cap; both are now enforced (120 req/min, 1 MiB), keyed per webhook instead of per IP (previously shared across every tenant behind the same load-balancer IP).
- A failed delivery permanently blocked its `event_id` from ever being retried; an update/delete mapping with a non-unique match key could silently write to the wrong row; a soft-deleted row could be resurrected by an update targeting a stale match; a webhook token could leak into application logs; six 500 responses across the row CRUD endpoints leaked raw Postgres error messages to the client instead of a generic message.
- Creating an `insert`-action row policy on any table (not just webhook-related) was broken outright by an invalid `USING` clause on `INSERT` policies.

## Upgrade notes

- No manual migration steps. Existing tables, policies, and RLS modes are unaffected.
- A webhook created before this release has a token that can no longer be decrypted for display; rotate it once to get a working link again.
