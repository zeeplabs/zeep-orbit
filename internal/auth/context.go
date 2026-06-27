package auth

import "context"

type contextKey int

const userContextKey contextKey = 0

// AuthUser holds authenticated user info extracted from JWT claims.
type AuthUser struct {
	ID    string
	Email string
	App   string
}

// WithUser returns a new context with the authenticated user injected.
func WithUser(ctx context.Context, u *AuthUser) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// UserFromContext retrieves the AuthUser from context.
func UserFromContext(ctx context.Context) (*AuthUser, bool) {
	u, ok := ctx.Value(userContextKey).(*AuthUser)
	return u, ok
}
