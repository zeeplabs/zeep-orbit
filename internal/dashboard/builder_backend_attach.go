package dashboard

// builder_backend_attach.go — the dashboard side of the attach_backend turn
// (builder-no-backend NB-08, T11d-2): the callback the worker runs after the
// overlay is committed. It creates the backend app (mode new) and sets the
// session's backend in one conditional update.

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// attachBackend is agent.AttachFunc. It runs as the session's owner and is safe
// to call again for the same turn: when an earlier attempt already attached a
// backend it returns that backend's app id.
func (h *BuilderHandler) attachBackend(ctx context.Context, sess *store.Session, p store.AttachPayload) (string, error) {
	if h.dashboard == nil {
		return "", errors.New("builder: attach backend: dashboard handler not configured")
	}
	// The session in the claim may be stale (an earlier attempt of this very turn
	// can have attached).
	cur, err := h.store.GetSession(ctx, sess.OwnerUserID, sess.ID)
	if err != nil {
		return "", fmt.Errorf("builder: attach backend: reload session: %w", err)
	}
	if cur.BackendAppID != "" {
		if p.Mode == store.AttachModeExisting && cur.BackendAppID != p.AppID {
			return "", store.ErrBackendAlreadyAttached
		}
		if p.Mode == store.AttachModeNew && cur.BackendOrigin != store.BackendOriginBuilder {
			return "", store.ErrBackendAlreadyAttached
		}
		return cur.BackendAppID, nil
	}

	user, err := GetUser(ctx, h.pool, sess.OwnerUserID)
	if err != nil {
		return "", fmt.Errorf("builder: attach backend: owner: %w", err)
	}

	switch p.Mode {
	case store.AttachModeNew:
		created, err := h.createBuilderBackend(ctx, user, p.AppName, "")
		if err != nil {
			return "", fmt.Errorf("builder: attach backend: create app: %w", err)
		}
		if err := h.store.AttachBackend(ctx, sess.ID, created.ID, store.BackendOriginBuilder); err != nil {
			h.removeCreatedBackend(ctx, user, created, "")
			return "", err
		}
		return created.ID, nil
	case store.AttachModeExisting:
		// The handler checked access when the turn was made; the user may have lost
		// it while the turn waited.
		if _, _, err := GetApp(ctx, h.pool, p.AppID, user); err != nil {
			h.logger.Warn("builder: attach backend: existing app not visible",
				zap.String("session_id", sess.ID), zap.String("app_id", p.AppID), zap.Error(err))
			return "", err
		}
		if err := h.store.AttachBackend(ctx, sess.ID, p.AppID, store.BackendOriginExisting); err != nil {
			return "", err
		}
		return p.AppID, nil
	}
	return "", fmt.Errorf("builder: attach backend: unknown mode %q", p.Mode)
}
