package dashboard

import (
	"context"
	"net/http"

	"github.com/zeeplabs/zeep-core/internal/db"
)

type dashCtxKey int

const userCtxKey dashCtxKey = 0

const cookieName = "zeep_session"

// UserFromContext retrieves the authenticated DashboardUser from context.
func UserFromContext(ctx context.Context) (*DashboardUser, bool) {
	u, ok := ctx.Value(userCtxKey).(*DashboardUser)
	return u, ok
}

// RequireAuth validates the zeep_session cookie and injects DashboardUser into context.
// Returns 401 JSON if missing, invalid, or expired.
func RequireAuth(pool *db.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			user, err := GetSessionUser(r.Context(), pool, cookie.Value)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
}
