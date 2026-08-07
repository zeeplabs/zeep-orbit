package auth

import (
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const TokenTTL = time.Hour

// Claims is the JWT payload emitted by zeep-orbit native auth.
type Claims struct {
	Email string `json:"email"`
	App   string `json:"app"`
	Role  string `json:"role"`
	jwtlib.RegisteredClaims
}

// IssueJWT signs and returns a new HS256 JWT for the given user.
func IssueJWT(secret []byte, userID, email, appName, role string) (string, error) {
	now := time.Now()
	c := Claims{
		Email: email,
		App:   appName,
		Role:  role,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(TokenTTL)),
		},
	}
	t := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	return t.SignedString(secret)
}

// ParseJWT validates an HS256 JWT and returns its claims.
func ParseJWT(secret []byte, raw string) (*Claims, error) {
	t, err := jwtlib.ParseWithClaims(raw, &Claims{}, func(t *jwtlib.Token) (any, error) {
		return secret, nil
	}, jwtlib.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return nil, jwtlib.ErrTokenSignatureInvalid
	}
	c, ok := t.Claims.(*Claims)
	if !ok {
		return nil, jwtlib.ErrTokenInvalidClaims
	}
	return c, nil
}

// AppTokenClaims is the JWT payload for app-managed tokens.
type AppTokenClaims struct {
	TokenType string `json:"token_type"`
	jwtlib.RegisteredClaims
}

// IssueAppTokenJWT signs and returns a new HS256 JWT for the given app token.
func IssueAppTokenJWT(secret []byte, jti, appName string, expiresAt *time.Time) (string, error) {
	now := time.Now()
	c := AppTokenClaims{
		TokenType: "app_token",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:  appName,
			ID:       jti,
			IssuedAt: jwtlib.NewNumericDate(now),
		},
	}
	if expiresAt != nil {
		c.ExpiresAt = jwtlib.NewNumericDate(*expiresAt)
	}
	t := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c)
	return t.SignedString(secret)
}

// ParseAppTokenJWT validates an HS256 JWT and returns its app token claims.
func ParseAppTokenJWT(secret []byte, raw string) (*AppTokenClaims, error) {
	t, err := jwtlib.ParseWithClaims(raw, &AppTokenClaims{}, func(t *jwtlib.Token) (any, error) {
		return secret, nil
	}, jwtlib.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return nil, jwtlib.ErrTokenSignatureInvalid
	}
	c, ok := t.Claims.(*AppTokenClaims)
	if !ok {
		return nil, jwtlib.ErrTokenInvalidClaims
	}
	return c, nil
}
