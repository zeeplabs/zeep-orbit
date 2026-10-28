package builder

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// MinSecretLen is the minimum accepted length (bytes) of the shared secret.
	// HMAC-SHA256 accepts any key length, so without a floor a one-character
	// BUILDER_SHARED_SECRET would "work" and be brute-forceable offline from a
	// single captured token. 16 bytes is a deliberately low floor that still
	// rejects typos/placeholders; deployments should use 32 random bytes.
	MinSecretLen = 16

	// RequestTokenTTL is how long an Orbit -> builder request token is valid.
	// Short because each token is minted per call and bound to one method and
	// path prefix; a leaked token is only useful for a minute.
	RequestTokenTTL = 60 * time.Second

	// PreviewTokenTTL is how long a browser preview token (carried in the preview URL path, /p/{token}/) is valid.
	PreviewTokenTTL = 10 * time.Minute

	// maxTokenLen bounds the work done on attacker-controlled input before any
	// MAC check. Legitimate tokens are a few hundred bytes.
	maxTokenLen = 2048

	// Distinct labels used to derive one sub-key per token purpose.
	requestKeyLabel = "builder-request-v1"
	previewKeyLabel = "builder-preview-v1"
)

var (
	// ErrInvalidToken is the only error VerifyRequest and VerifyPreview
	// return. It is deliberately uniform so callers (and therefore HTTP
	// responses) never reveal which check failed.
	ErrInvalidToken = errors.New("builder: invalid token")

	// ErrSecretEmpty is returned by NewSigner when the secret is empty.
	ErrSecretEmpty = errors.New("builder: shared secret is empty")

	// ErrSecretTooShort is returned by NewSigner when the secret is shorter
	// than MinSecretLen bytes.
	ErrSecretTooShort = errors.New("builder: shared secret is too short")

	// ErrInvalidClaims is returned by the Sign* methods for empty or malformed
	// inputs (empty session id, path prefix not starting with "/", ...).
	ErrInvalidClaims = errors.New("builder: invalid token claims")
)

// Internal verification reasons. They exist so tests can assert which check
// rejected a token; they are never returned by the exported methods.
var (
	errReasonMalformed = errors.New("malformed token")
	errReasonSignature = errors.New("bad signature")
	errReasonClaims    = errors.New("bad claims")
	errReasonExpired   = errors.New("expired")
	errReasonSession   = errors.New("session mismatch")
	errReasonMethod    = errors.New("method mismatch")
	errReasonPath      = errors.New("path mismatch")
)

// RequestClaims is the signed payload of an Orbit -> builder request token
// (header X-Builder-Token).
type RequestClaims struct {
	SessionID  string `json:"sid"`
	Method     string `json:"m"`
	PathPrefix string `json:"p"`
	ExpiresAt  int64  `json:"exp"`
}

// PreviewClaims is the signed payload of a preview token (carried in the iframe URL path). The
// caller of VerifyPreview must compare SessionID and UserID against the
// request it is serving.
type PreviewClaims struct {
	SessionID string `json:"sid"`
	UserID    string `json:"uid"`
	ExpiresAt int64  `json:"exp"`
}

// Signer mints and verifies builder tokens. It holds only derived keys and an
// immutable clock, so it is safe for concurrent use and keeps no per-process
// state: any Orbit replica or builder instance with the same secret accepts
// tokens minted by any other (the service runs behind a non-sticky load
// balancer).
//
// Domain separation: request and preview tokens share the wire format but are
// MACed with different keys, each derived as HMAC-SHA256(secret, label). A
// preview token therefore fails the MAC check when presented as a request
// token (and vice versa) regardless of what its claims say. Key derivation was
// chosen over an in-payload "type" claim because the guarantee holds at the
// cryptographic layer and cannot be lost by a future claim-parsing change.
type Signer struct {
	requestKey []byte
	previewKey []byte
	now        func() time.Time // injectable for tests
}

// NewSigner builds a Signer from the shared secret (BUILDER_SHARED_SECRET). It
// refuses an empty secret and one shorter than MinSecretLen bytes. Errors never
// include the secret or its length. The secret is copied, so later mutation of
// the caller's slice has no effect.
func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, ErrSecretEmpty
	}
	if len(secret) < MinSecretLen {
		return nil, ErrSecretTooShort
	}
	return &Signer{
		requestKey: deriveKey(secret, requestKeyLabel),
		previewKey: deriveKey(secret, previewKeyLabel),
		now:        time.Now,
	}, nil
}

func deriveKey(secret []byte, label string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// SignRequest mints a request token bound to the session id, the HTTP method
// and a path prefix, valid for RequestTokenTTL. pathPrefix must start with "/";
// a trailing "/" is ignored. The prefix matches whole path segments only (see
// VerifyRequest).
func (s *Signer) SignRequest(sessionID, method, pathPrefix string) (string, error) {
	pathPrefix = strings.TrimRight(pathPrefix, "/")
	if sessionID == "" || method == "" || !strings.HasPrefix(pathPrefix, "/") {
		return "", ErrInvalidClaims
	}
	return seal(s.requestKey, RequestClaims{
		SessionID:  sessionID,
		Method:     strings.ToUpper(method),
		PathPrefix: pathPrefix,
		ExpiresAt:  s.now().Add(RequestTokenTTL).Unix(),
	})
}

// VerifyRequest checks a request token against the session id, method and path
// of the request being served. The MAC is verified first (constant time), then
// expiry, session id, method and path. The signed prefix matches on a segment
// boundary: "/v1/sessions/abc" accepts "/v1/sessions/abc" and
// "/v1/sessions/abc/ops" but not "/v1/sessions/abcd/ops". Paths with "." or
// ".." segments or a doubled slash are rejected so a signed prefix cannot be
// escaped by traversal. Any failure returns ErrInvalidToken.
func (s *Signer) VerifyRequest(token, sessionID, method, path string) error {
	if err := s.verifyRequest(token, sessionID, method, path); err != nil {
		return ErrInvalidToken
	}
	return nil
}

func (s *Signer) verifyRequest(token, sessionID, method, path string) error {
	var c RequestClaims
	if err := open(s.requestKey, token, &c); err != nil {
		return err
	}
	if c.SessionID == "" || c.Method == "" || !strings.HasPrefix(c.PathPrefix, "/") {
		return errReasonClaims
	}
	if s.now().Unix() > c.ExpiresAt {
		return errReasonExpired
	}
	if sessionID == "" || !constantTimeEqual(c.SessionID, sessionID) {
		return errReasonSession
	}
	if c.Method != strings.ToUpper(method) {
		return errReasonMethod
	}
	if !pathHasPrefix(path, c.PathPrefix) {
		return errReasonPath
	}
	return nil
}

// SignPreview mints a preview token bound to a session id and a user id, valid
// for PreviewTokenTTL.
func (s *Signer) SignPreview(sessionID, userID string) (string, error) {
	if sessionID == "" || userID == "" {
		return "", ErrInvalidClaims
	}
	return seal(s.previewKey, PreviewClaims{
		SessionID: sessionID,
		UserID:    userID,
		ExpiresAt: s.now().Add(PreviewTokenTTL).Unix(),
	})
}

// VerifyPreview checks the MAC and expiry of a preview token and returns its
// claims. It does not know which session or user is being served, so the caller
// must compare SessionID and UserID itself. Any failure returns ErrInvalidToken.
func (s *Signer) VerifyPreview(token string) (PreviewClaims, error) {
	var c PreviewClaims
	if err := open(s.previewKey, token, &c); err != nil {
		return PreviewClaims{}, ErrInvalidToken
	}
	if c.SessionID == "" || c.UserID == "" || s.now().Unix() > c.ExpiresAt {
		return PreviewClaims{}, ErrInvalidToken
	}
	return c, nil
}

// seal serializes claims as base64url(json) "." base64url(HMAC-SHA256), the
// same construction as signState in internal/auth/google.go.
func seal(key []byte, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("builder: marshal claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + macOf(key, encoded), nil
}

// open verifies the MAC of token under key and decodes the claims into out. The
// MAC is checked before the payload is decoded so unauthenticated input never
// reaches the JSON parser. Expiry and binding checks are the caller's job.
func open(key []byte, token string, out any) error {
	if token == "" || len(token) > maxTokenLen {
		return errReasonMalformed
	}
	encoded, sig, found := strings.Cut(token, ".")
	if !found || encoded == "" || sig == "" || strings.Contains(sig, ".") {
		return errReasonMalformed
	}
	// Compare the encoded forms: base64 decoding is not canonical (trailing
	// bits are ignored), so comparing decoded bytes would accept more than one
	// string per MAC.
	if !hmac.Equal([]byte(sig), []byte(macOf(key, encoded))) {
		return errReasonSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errReasonMalformed
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return errReasonClaims
	}
	return nil
}

func macOf(key []byte, encodedPayload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func constantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// pathHasPrefix reports whether path equals prefix or lies under it on a
// segment boundary. prefix has no trailing slash.
func pathHasPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
