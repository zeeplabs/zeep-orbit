package dashboard

// builder_backend_create.go — which backend a new builder session talks to
// (ai-builder-backend B02, BBE-01 to BBE-03): an existing Orbit app, or a new
// empty one the builder creates together with the session.

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"
)

const (
	builderBackendModeNew      = "new"
	builderBackendModeExisting = "existing"

	// builderDefaultSessionTitle names a new-backend session created without a
	// title. API strings are English; the UI shows its own placeholder.
	builderDefaultSessionTitle = "Untitled app"

	// builderAppNameBaseMax leaves room for "-" and the 6-hex suffix inside the
	// 32-character app name limit (appNameRe).
	builderAppNameBaseMax = 25
)

// builderBackendRequest is the optional "backend" object of the create-session
// body. A body with only the legacy backend_app_id still means "existing".
type builderBackendRequest struct {
	Mode  string `json:"mode"`
	AppID string `json:"app_id"`
}

// resolveBuilderBackend validates the backend selection of a create-session
// body and returns the mode and (for "existing") the app id as sent. The error
// text is safe to return to the client.
func resolveBuilderBackend(b *builderBackendRequest, legacyAppID string) (mode, appID string, err error) {
	legacy := strings.TrimSpace(legacyAppID)
	if b == nil {
		return builderBackendModeExisting, legacy, nil
	}
	switch strings.TrimSpace(b.Mode) {
	case builderBackendModeNew:
		if legacy != "" || strings.TrimSpace(b.AppID) != "" {
			return "", "", errors.New(`backend mode "new" cannot be combined with an app id`)
		}
		return builderBackendModeNew, "", nil
	case builderBackendModeExisting:
		id := strings.TrimSpace(b.AppID)
		switch {
		case id == "":
			id = legacy
		case legacy != "" && !strings.EqualFold(legacy, id):
			return "", "", errors.New("backend.app_id and backend_app_id differ")
		}
		return builderBackendModeExisting, id, nil
	default:
		return "", "", errors.New(`backend mode must be "new" or "existing"`)
	}
}

// latinFold maps the Latin-1 accented letters common in Portuguese, Spanish and
// French titles to their ASCII base, so "Clínica São João" becomes
// "clinica-sao-joao" instead of "cl-nica-s-o-jo-o". No new dependency.
var latinFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// deriveBuilderAppName builds a valid, unique-looking Orbit app name from a
// session title: ASCII lowercase letters, digits and hyphens, starting with a
// letter, at most 32 characters, ending in "-<suffix>". It never fails: a title
// with no usable characters falls back to "app".
func deriveBuilderAppName(title, suffix string) string {
	var b strings.Builder
	dash := true // collapses runs and trims leading separators
	for _, r := range latinFold.Replace(strings.ToLower(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "app"
	}
	if base[0] < 'a' || base[0] > 'z' {
		base = "app-" + base
	}
	if len(base) > builderAppNameBaseMax {
		base = strings.TrimRight(base[:builderAppNameBaseMax], "-")
	}
	return base + "-" + suffix
}

// createBuilderBackend creates the empty backend app for a new-backend session
// through the same operation as POST /api/apps (CreateAppForUser): same
// validation, provisioning, registry refresh and app.create audit row. Email
// auth is on so the agent can propose owner-scoped access later.
func (h *BuilderHandler) createBuilderBackend(ctx context.Context, user *DashboardUser, name, ip string) (*AppRow, error) {
	return h.dashboard.CreateAppForUser(ctx, user, AppRequestBody{Name: name, AuthEmailEnabled: true}, ip)
}

// removeCreatedBackend is the best-effort compensation for a new-backend create
// that failed after the app was made (BBE-02). It mirrors the DELETE app
// handler (store delete, registry unregister, app.delete audit) and only ever
// receives the app this request just created, which is empty. A failure is
// logged and not surfaced: the leftover is an empty app the user can delete.
// Like every app delete, it leaves the provisioned (empty) Postgres schema.
func (h *BuilderHandler) removeCreatedBackend(ctx context.Context, user *DashboardUser, app *AppRow, ip string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
	defer cancel()
	if err := DeleteApp(cctx, h.pool, app.ID, user); err != nil {
		h.logger.Error("builder: remove backend of failed session create",
			zap.String("app_id", app.ID), zap.Error(err))
		return
	}
	h.dashboard.reg.Unregister(app.Name)
	h.dashboard.audit(cctx, user.ID, user.Email, "app.delete", "app", app.ID, app.Name,
		[]byte(`{"reason":"builder_session_create_failed"}`), ip)
}
