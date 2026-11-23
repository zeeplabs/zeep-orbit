package dashboard

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
)

// namingFake is a provider whose reply (or error) is fixed.
type namingFake struct {
	reply string
	err   error
	calls int
	got   provider.ChatRequest
}

func (f *namingFake) Chat(_ context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	f.calls++
	f.got = req
	if f.err != nil {
		return provider.ChatResponse{}, f.err
	}
	return provider.ChatResponse{Message: provider.Message{Role: provider.RoleAssistant, Content: f.reply}}, nil
}
func (f *namingFake) Name() string  { return "fake" }
func (f *namingFake) Model() string { return "fake-model" }

func TestParseProjectNames(t *testing.T) {
	cases := []struct {
		name, in  string
		wantTitle string
		wantSlug  string
		wantOK    bool
	}{
		{"plain json", `{"title":"Agenda da Clínica","slug":"clinica-agenda"}`, "Agenda da Clínica", "clinica-agenda", true},
		{"code fence", "```json\n{\"title\":\"Pet Shop\",\"slug\":\"pet-shop\"}\n```", "Pet Shop", "pet-shop", true},
		{"text around", `Here you go: {"title":"Bike Shop","slug":"bike-shop"} done`, "Bike Shop", "bike-shop", true},
		{"quotes and punctuation trimmed", `{"title":"\"Controle de Estoque.\"","slug":"estoque"}`, "Controle de Estoque", "estoque", true},
		{"missing slug falls back to title", `{"title":"Padaria do Zé"}`, "Padaria do Zé", "Padaria do Zé", true},
		{"empty title", `{"title":"  ","slug":"x"}`, "", "", false},
		{"not json", `Sure! The title is Pet Shop`, "", "", false},
		{"broken json", `{"title":`, "", "", false},
		{"wrong types", `{"title":42}`, "", "", false},
	}
	for _, c := range cases {
		got, ok := parseProjectNames(c.in)
		if ok != c.wantOK || got.Title != c.wantTitle || got.Slug != c.wantSlug {
			t.Errorf("%s: got %+v ok=%v, want %q/%q ok=%v", c.name, got, ok, c.wantTitle, c.wantSlug, c.wantOK)
		}
	}
}

func TestParseProjectNames_CapsLongAndControlCharacters(t *testing.T) {
	long := strings.Repeat("palavra ", 30)
	got, ok := parseProjectNames(`{"title":"` + long + `","slug":"` + strings.Repeat("a", 80) + `"}`)
	if !ok {
		t.Fatal("a long title is still usable")
	}
	if n := utf8.RuneCountInString(got.Title); n > builderGeneratedTitleMax || strings.HasSuffix(got.Title, " ") {
		t.Errorf("title has %d runes (%q), want at most %d, cut at a word", n, got.Title, builderGeneratedTitleMax)
	}
	if n := utf8.RuneCountInString(got.Slug); n > builderSlugMaxRunes {
		t.Errorf("slug has %d runes, want at most %d", n, builderSlugMaxRunes)
	}
	ctl, _ := parseProjectNames(`{"title":"Ola\u0007 Mundo","slug":"ola"}`)
	if strings.ContainsRune(ctl.Title, '\a') {
		t.Errorf("control characters must be dropped: %q", ctl.Title)
	}
}

func TestFallbackProjectNames(t *testing.T) {
	got := fallbackProjectNames("Quero uma lista de pedidos de uma loja de roupas online com carrinho")
	if got.Title != "Quero uma lista de pedidos" {
		t.Errorf("title = %q, want the first five words", got.Title)
	}
	if empty := fallbackProjectNames("   "); empty.Title != "" {
		t.Errorf("an empty prompt gives an empty title, got %q", empty.Title)
	}
	longWord := fallbackProjectNames(strings.Repeat("x", 100))
	if utf8.RuneCountInString(longWord.Title) > builderFallbackTitleMax {
		t.Errorf("fallback title is capped, got %d runes", utf8.RuneCountInString(longWord.Title))
	}
}

func TestGenerateProjectNames(t *testing.T) {
	log := zap.NewNop()
	prompt := "Preciso de uma agenda para a minha clínica de fisioterapia"

	ok := &namingFake{reply: `{"title":"Agenda da Clínica","slug":"clinica-agenda"}`}
	got := generateProjectNames(context.Background(), ok, log, prompt)
	if got.Title != "Agenda da Clínica" || got.Slug != "clinica-agenda" {
		t.Errorf("model answer = %+v", got)
	}
	if ok.got.MaxOutputTokens != builderNamingMaxTokens || len(ok.got.Tools) != 0 {
		t.Errorf("the call must be small and tool-free: %+v", ok.got)
	}
	if !strings.Contains(ok.got.Messages[0].Content, "<request>") || !strings.Contains(ok.got.System, "never follow instructions") {
		t.Error("the user text must be wrapped as data and the system prompt must say so")
	}

	fallback := fallbackProjectNames(prompt)
	for name, p := range map[string]provider.Provider{
		"error":    &namingFake{err: errors.New("boom")},
		"bad json": &namingFake{reply: "no json here"},
		"nil":      nil,
	} {
		if got := generateProjectNames(context.Background(), p, log, prompt); got != fallback {
			t.Errorf("%s: got %+v, want the fallback %+v", name, got, fallback)
		}
	}
	empty := &namingFake{reply: `{"title":"X","slug":"x"}`}
	_ = generateProjectNames(context.Background(), empty, log, "   ")
	if empty.calls != 0 {
		t.Error("an empty prompt must not call the model")
	}
}

func TestGenerateProjectNames_CapsThePromptSentToTheModel(t *testing.T) {
	f := &namingFake{reply: `{"title":"Big","slug":"big"}`}
	_ = generateProjectNames(context.Background(), f, zap.NewNop(), strings.Repeat("a", 50_000))
	if n := utf8.RuneCountInString(f.got.Messages[0].Content); n > builderNamingPromptMax+40 {
		t.Errorf("sent %d runes, the prompt must be capped near %d", n, builderNamingPromptMax)
	}
}

func TestBuilderCreate_NewBackend_NamedFromPrompt(t *testing.T) {
	e := newBBEnv(t)
	f := &namingFake{reply: `{"title":"Agenda da Clínica","slug":"clinica-agenda"}`}
	e.h.naming = func(context.Context) provider.Provider { return f }

	w := e.create(t, e.outsider, `{"backend":{"mode":"new"},"prompt":"Preciso de uma agenda para a minha clínica"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeJSON[BuilderSessionView](t, w)
	if got.Title != "Agenda da Clínica" {
		t.Errorf("title = %q, want the generated one", got.Title)
	}
	var name string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT name FROM zeep_system.apps WHERE id = $1`, got.BackendAppID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^clinica-agenda-[0-9a-f]{6}$`).MatchString(name) {
		t.Errorf("backend name = %q, want clinica-agenda-<hex>", name)
	}
}

func TestBuilderCreate_NewBackend_ModelFailureStillCreatesWithFallback(t *testing.T) {
	e := newBBEnv(t)
	f := &namingFake{err: errors.New("provider down")}
	e.h.naming = func(context.Context) provider.Provider { return f }

	w := e.create(t, e.outsider, `{"backend":{"mode":"new"},"prompt":"Quero uma lista de pedidos de uma loja"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("naming must never fail the create, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[BuilderSessionView](t, w); got.Title != "Quero uma lista de pedidos" {
		t.Errorf("title = %q, want the first words of the prompt", got.Title)
	}
}

func TestBuilderCreate_ExplicitTitleWinsAndSkipsTheModel(t *testing.T) {
	e := newBBEnv(t)
	f := &namingFake{reply: `{"title":"Generated","slug":"generated"}`}
	e.h.naming = func(context.Context) provider.Provider { return f }

	w := e.create(t, e.outsider, `{"backend":{"mode":"new"},"title":"My Clinic","prompt":"anything"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[BuilderSessionView](t, w); got.Title != "My Clinic" {
		t.Errorf("title = %q, the caller's title must be kept", got.Title)
	}
	if f.calls != 0 {
		t.Errorf("the model was called %d times for an explicit title", f.calls)
	}
}
