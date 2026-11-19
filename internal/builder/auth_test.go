package builder

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	testSecret = []byte("0123456789abcdef0123456789abcdef")
	testEpoch  = time.Unix(1_700_000_000, 0)
)

// newTestSigner returns a Signer whose clock is pinned to *now.
func newTestSigner(t *testing.T, secret []byte, now *time.Time) *Signer {
	t.Helper()
	s, err := NewSigner(secret)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	s.now = func() time.Time { return *now }
	return s
}

func TestNewSignerRefusesWeakSecrets(t *testing.T) {
	tests := []struct {
		name   string
		secret []byte
		want   error
	}{
		{"nil", nil, ErrSecretEmpty},
		{"empty", []byte{}, ErrSecretEmpty},
		{"one byte", []byte("x"), ErrSecretTooShort},
		{"fifteen bytes", []byte(strings.Repeat("a", MinSecretLen-1)), ErrSecretTooShort},
		{"exactly min", []byte(strings.Repeat("a", MinSecretLen)), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSigner(tc.secret)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil && s != nil {
				t.Fatal("expected nil Signer on error")
			}
			if err != nil && strings.Contains(err.Error(), string(tc.secret)) && len(tc.secret) > 0 {
				t.Fatal("error leaks the secret")
			}
		})
	}
}

func TestNewSignerCopiesSecret(t *testing.T) {
	secret := []byte(strings.Repeat("k", 32))
	now := testEpoch
	s, err := NewSigner(secret)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	tok, err := s.SignRequest("abc", "POST", "/v1/sessions/abc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range secret {
		secret[i] = 0
	}
	if err := s.VerifyRequest(tok, "abc", "POST", "/v1/sessions/abc/ops"); err != nil {
		t.Fatalf("token rejected after caller mutated its secret slice: %v", err)
	}
}

func TestRequestToken(t *testing.T) {
	const (
		sid    = "abc"
		prefix = "/v1/sessions/abc"
	)
	otherSecret := []byte("ffffffffffffffffffffffffffffffff")

	tests := []struct {
		name    string
		sign    func(s *Signer) (string, error)
		verifyS func(t *testing.T, now *time.Time) *Signer // verifying signer; nil = same secret
		advance time.Duration
		sid     string
		method  string
		path    string
		want    error // internal reason; nil = accepted
	}{
		{
			name:   "valid exact prefix",
			sid:    sid,
			method: "POST",
			path:   prefix,
		},
		{
			name:   "valid nested path",
			sid:    sid,
			method: "POST",
			path:   prefix + "/ops",
		},
		{
			name:   "valid method case-insensitive",
			sid:    sid,
			method: "post",
			path:   prefix + "/ops",
		},
		{
			name:    "valid at exactly ttl",
			advance: RequestTokenTTL,
			sid:     sid,
			method:  "POST",
			path:    prefix + "/ops",
		},
		{
			name:    "expired",
			advance: RequestTokenTTL + time.Second,
			sid:     sid,
			method:  "POST",
			path:    prefix + "/ops",
			want:    errReasonExpired,
		},
		{
			name: "wrong secret",
			verifyS: func(t *testing.T, now *time.Time) *Signer {
				return newTestSigner(t, otherSecret, now)
			},
			sid:    sid,
			method: "POST",
			path:   prefix + "/ops",
			want:   errReasonSignature,
		},
		{
			name:   "wrong session",
			sid:    "xyz",
			method: "POST",
			path:   prefix + "/ops",
			want:   errReasonSession,
		},
		{
			name:   "empty session in request",
			sid:    "",
			method: "POST",
			path:   prefix + "/ops",
			want:   errReasonSession,
		},
		{
			name:   "wrong method",
			sid:    sid,
			method: "DELETE",
			path:   prefix + "/ops",
			want:   errReasonMethod,
		},
		{
			name:   "wrong path other tree",
			sid:    sid,
			method: "POST",
			path:   "/v1/sessions/other/ops",
			want:   errReasonPath,
		},
		{
			name:   "segment boundary abc vs abcd",
			sid:    sid,
			method: "POST",
			path:   "/v1/sessions/abcd/ops",
			want:   errReasonPath,
		},
		{
			name:   "segment boundary abc vs abcd exact",
			sid:    sid,
			method: "POST",
			path:   "/v1/sessions/abcd",
			want:   errReasonPath,
		},
		{
			name:   "traversal out of prefix",
			sid:    sid,
			method: "POST",
			path:   prefix + "/../xyz/ops",
			want:   errReasonPath,
		},
		{
			name:   "double slash",
			sid:    sid,
			method: "POST",
			path:   prefix + "//ops",
			want:   errReasonPath,
		},
		{
			name:   "shorter than prefix",
			sid:    sid,
			method: "POST",
			path:   "/v1/sessions",
			want:   errReasonPath,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := testEpoch
			signer := newTestSigner(t, testSecret, &now)
			tok, err := signer.SignRequest(sid, "POST", prefix)
			if err != nil {
				t.Fatalf("SignRequest: %v", err)
			}
			verifier := signer
			if tc.verifyS != nil {
				verifier = tc.verifyS(t, &now)
			}
			now = now.Add(tc.advance)

			gotInternal := verifier.verifyRequest(tok, tc.sid, tc.method, tc.path)
			if !errors.Is(gotInternal, tc.want) && !(tc.want == nil && gotInternal == nil) {
				t.Fatalf("internal reason = %v, want %v", gotInternal, tc.want)
			}

			gotPublic := verifier.VerifyRequest(tok, tc.sid, tc.method, tc.path)
			if tc.want == nil {
				if gotPublic != nil {
					t.Fatalf("VerifyRequest = %v, want nil", gotPublic)
				}
				return
			}
			if gotPublic != ErrInvalidToken {
				t.Fatalf("VerifyRequest = %v, want exactly ErrInvalidToken", gotPublic)
			}
		})
	}
}

func TestSignRequestTrailingSlashPrefix(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)
	tok, err := s.SignRequest("abc", "GET", "/v1/sessions/abc/")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyRequest(tok, "abc", "GET", "/v1/sessions/abc/files"); err != nil {
		t.Fatalf("trailing slash prefix should match nested path: %v", err)
	}
	if err := s.VerifyRequest(tok, "abc", "GET", "/v1/sessions/abcd/files"); err == nil {
		t.Fatal("trailing slash prefix must still respect the segment boundary")
	}
}

func TestSignRequestRejectsInvalidInputs(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)
	tests := []struct {
		name, sid, method, prefix string
	}{
		{"empty session", "", "GET", "/v1/x"},
		{"empty method", "abc", "", "/v1/x"},
		{"empty prefix", "abc", "GET", ""},
		{"root prefix would match everything", "abc", "GET", "/"},
		{"relative prefix", "abc", "GET", "v1/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := s.SignRequest(tc.sid, tc.method, tc.prefix)
			if !errors.Is(err, ErrInvalidClaims) || tok != "" {
				t.Fatalf("got (%q, %v), want ErrInvalidClaims", tok, err)
			}
		})
	}
}

func TestRequestTokenTampering(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)
	tok, err := s.SignRequest("abc", "GET", "/v1/sessions/abc")
	if err != nil {
		t.Fatal(err)
	}
	encoded, sig, _ := strings.Cut(tok, ".")

	// forged claims signed with a key the attacker does not have: reuse the
	// legitimate signature over a modified payload.
	forged := RequestClaims{SessionID: "abc", Method: "DELETE", PathPrefix: "/v1/sessions/abc", ExpiresAt: testEpoch.Add(time.Hour).Unix()}
	forgedPayload, _ := json.Marshal(forged)
	forgedEncoded := base64.RawURLEncoding.EncodeToString(forgedPayload)

	tests := []struct {
		name  string
		token string
	}{
		{"flip payload char", flipChar(encoded) + "." + sig},
		{"flip signature char", encoded + "." + flipChar(sig)},
		{"old signature on new payload", forgedEncoded + "." + sig},
		{"truncated signature", encoded + "." + sig[:len(sig)-2]},
		{"signature with extra suffix", encoded + "." + sig + "A"},
		{"extra dot segment", tok + ".x"},
		{"swapped parts", sig + "." + encoded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.VerifyRequest(tc.token, "abc", "GET", "/v1/sessions/abc/ops"); err != ErrInvalidToken {
				t.Fatalf("got %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestMalformedTokens(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)

	// Validly MACed (request key) but undecodable payloads: exercises the
	// post-MAC decode paths.
	badBase64 := "!!!not-base64!!!"
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	wrongShape := base64.RawURLEncoding.EncodeToString([]byte(`{"sid":123}`))

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no dot", "abcdef"},
		{"empty payload", ".sig"},
		{"empty signature", "payload."},
		{"only dot", "."},
		{"bad base64 payload with valid mac", badBase64 + "." + macOf(s.requestKey, badBase64)},
		{"bad json with valid mac", notJSON + "." + macOf(s.requestKey, notJSON)},
		{"wrong json types with valid mac", wrongShape + "." + macOf(s.requestKey, wrongShape)},
		{"oversized", strings.Repeat("a", maxTokenLen+1) + ".b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.VerifyRequest(tc.token, "abc", "GET", "/v1/sessions/abc"); err != ErrInvalidToken {
				t.Fatalf("VerifyRequest = %v, want ErrInvalidToken", err)
			}
			if _, err := s.VerifyPreview(tc.token); err != ErrInvalidToken {
				t.Fatalf("VerifyPreview = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestPreviewToken(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)

	tok, err := s.SignPreview("sess-1", "user-1")
	if err != nil {
		t.Fatalf("SignPreview: %v", err)
	}

	t.Run("valid returns claims", func(t *testing.T) {
		c, err := s.VerifyPreview(tok)
		if err != nil {
			t.Fatalf("VerifyPreview: %v", err)
		}
		want := PreviewClaims{SessionID: "sess-1", UserID: "user-1", ExpiresAt: testEpoch.Add(PreviewTokenTTL).Unix()}
		if c != want {
			t.Fatalf("claims = %+v, want %+v", c, want)
		}
	})

	t.Run("expired", func(t *testing.T) {
		later := testEpoch.Add(PreviewTokenTTL + time.Second)
		exp := newTestSigner(t, testSecret, &later)
		if _, err := exp.VerifyPreview(tok); err != ErrInvalidToken {
			t.Fatalf("got %v, want ErrInvalidToken", err)
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		other := newTestSigner(t, []byte("ffffffffffffffffffffffffffffffff"), &now)
		if _, err := other.VerifyPreview(tok); err != ErrInvalidToken {
			t.Fatalf("got %v, want ErrInvalidToken", err)
		}
	})

	t.Run("tampered", func(t *testing.T) {
		if _, err := s.VerifyPreview(flipChar(tok)); err != ErrInvalidToken {
			t.Fatalf("got %v, want ErrInvalidToken", err)
		}
	})

	t.Run("sign rejects empty ids", func(t *testing.T) {
		if _, err := s.SignPreview("", "u"); !errors.Is(err, ErrInvalidClaims) {
			t.Fatalf("empty session: %v", err)
		}
		if _, err := s.SignPreview("s", ""); !errors.Is(err, ErrInvalidClaims) {
			t.Fatalf("empty user: %v", err)
		}
	})
}

func TestTokenDomainSeparation(t *testing.T) {
	now := testEpoch
	s := newTestSigner(t, testSecret, &now)

	previewTok, err := s.SignPreview("abc", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	reqTok, err := s.SignRequest("abc", "GET", "/v1/sessions/abc")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.VerifyRequest(previewTok, "abc", "GET", "/v1/sessions/abc"); err != ErrInvalidToken {
		t.Fatalf("preview token accepted as request token: %v", err)
	}
	if _, err := s.VerifyPreview(reqTok); err != ErrInvalidToken {
		t.Fatalf("request token accepted as preview token: %v", err)
	}

	// Even a payload crafted to satisfy both claim shapes must not cross over,
	// because the MAC keys differ.
	both := `{"sid":"abc","uid":"u","m":"GET","p":"/v1/sessions/abc","exp":` + itoa(testEpoch.Add(time.Hour).Unix()) + `}`
	enc := base64.RawURLEncoding.EncodeToString([]byte(both))
	asPreview := enc + "." + macOf(s.previewKey, enc)
	if err := s.VerifyRequest(asPreview, "abc", "GET", "/v1/sessions/abc"); err != ErrInvalidToken {
		t.Fatalf("dual-shape preview-MAC token accepted as request token: %v", err)
	}
	asRequest := enc + "." + macOf(s.requestKey, enc)
	if _, err := s.VerifyPreview(asRequest); err != ErrInvalidToken {
		t.Fatalf("dual-shape request-MAC token accepted as preview token: %v", err)
	}
}

func flipChar(s string) string {
	b := []byte(s)
	i := len(b) / 2
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
