package store

// publish.go — the publish bookkeeping of a session (ai-frontend-builder T32,
// AFB-22): which frontend app it was published as and which GitHub commit is
// the last one the builder pushed. Kept apart from store.go so publishing can
// evolve without touching the turn queue.

import (
	"context"
	"fmt"
)

// RecordPublish stores the result of a publish on the owner's session:
// frontendAppID links the session to the frontend app it was published as (nil
// keeps the existing link, used by a republish) and publishedSHA is the GitHub
// commit the builder pushed, the expected parent of the next republish. It does
// not touch the session status: a publish is not a turn.
func (s *Store) RecordPublish(ctx context.Context, ownerUserID, sessionID string, frontendAppID *string, publishedSHA string) error {
	if publishedSHA == "" {
		return fmt.Errorf("%w: published sha is required", ErrInvalidArgument)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE zeep_system.builder_sessions
		 SET frontend_app_id = COALESCE($3::uuid, frontend_app_id), published_sha = $4, updated_at = now()
		 WHERE id = $1 AND owner_user_id = $2`,
		sessionID, ownerUserID, frontendAppID, publishedSHA,
	)
	if err != nil {
		return fmt.Errorf("builder store: record publish: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
