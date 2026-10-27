package dashboard

// builder_runtime.go — the one place that turns the BUILDER_* environment into
// a running AI Frontend Builder (ai-frontend-builder T30): the sandbox client,
// the tool registry, the turn worker and the per-turn resolution of provider,
// limits and prompt. cmd/zeep/main.go only calls NewBuilderRuntimeFromEnv,
// Start and Stop; internal/server only receives HandlerConfig.
//
// Off switch: BUILDER_URL empty means NewBuilderRuntime returns (nil, nil).
// Every method of a nil *BuilderRuntime is a no-op and HandlerConfig then
// yields a config with an untyped-nil Sandbox, so the builder routes answer
// 503 "builder not configured", there is no worker goroutine and no sandbox
// client.
//
// What is resolved when:
//   - at start-up: builder URL, shared secret (fatal when missing or too
//     short), sandbox client, tools, worker;
//   - at the start of EVERY turn (agent.WorkerConfig.PrepareDeps): the
//     builder_settings row (provider choice and limits) and the provider row
//     (model, key, base URL), so a superadmin change applies to the next turn
//     without a restart. A provider that cannot be resolved does not abort the
//     worker: the turn runs with a provider whose Chat fails, which the loop
//     reports as provider_unavailable;
//   - at the start of every chat turn (PromptSource): the company layer pinned
//     on the session, the template guide and file tree read from the session's
//     workspace.

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
	"github.com/zeeplabs/zeep-orbit/internal/agent/provider"
	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
	"github.com/zeeplabs/zeep-orbit/internal/builder"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Environment variables read by the builder wiring.
const (
	// EnvBuilderURL is the builder daemon's base URL. Empty disables the feature.
	EnvBuilderURL = "BUILDER_URL"
	// EnvBuilderSharedSecret is the HMAC key shared with the builder (request and
	// preview tokens). Required when BUILDER_URL is set.
	EnvBuilderSharedSecret = "BUILDER_SHARED_SECRET"
)

const (
	// builderPromptScanTimeout bounds the archive read that supplies the
	// template guide and the file tree.
	builderPromptScanTimeout = 20 * time.Second

	maxGuideBytes      = 64 << 10
	maxScanEntries     = 5000
	maxTreeDepth       = 3
	maxTreeLines       = 300
	templateGuideFile  = "ORBIT_AGENT.md"
	providerUnavailMsg = "builder model provider is not available"
)

// BuilderRuntimeConfig configures NewBuilderRuntime.
type BuilderRuntimeConfig struct {
	// URL is BUILDER_URL; empty disables the builder (NewBuilderRuntime returns nil).
	URL string
	// SharedSecret is BUILDER_SHARED_SECRET (required when URL is set).
	SharedSecret string
	// Concurrency is the turn worker's slots; <1 means agent.WorkerConcurrencyFromEnv().
	Concurrency int
	// Logger receives the worker's and the runtime's server-side logs
	// (default slog.Default()).
	Logger *slog.Logger
	// HTTPClient is the client for the builder daemon (default: a plain client;
	// builder calls are bounded by their contexts, not a client timeout).
	HTTPClient *http.Client
}

// BuilderRuntime is the running builder: see the file comment. Build one with
// NewBuilderRuntime or NewBuilderRuntimeFromEnv.
type BuilderRuntime struct {
	pool    *db.Pool
	store   *store.Store
	sandbox sandbox.Sandbox // the restoring wrapper, shared by handlers, tools and worker
	signer  *builder.Signer
	worker  *agent.Worker
	log     *slog.Logger

	// providerHTTP is the SSRF-guarded client handed to the LLM adapter. Tests
	// replace it; nothing outside this package can.
	providerHTTP *http.Client

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewBuilderRuntimeFromEnv reads BUILDER_URL, BUILDER_SHARED_SECRET and
// BUILDER_WORKER_CONCURRENCY. It returns (nil, nil) when BUILDER_URL is empty.
func NewBuilderRuntimeFromEnv(pool *db.Pool, logger *slog.Logger) (*BuilderRuntime, error) {
	return NewBuilderRuntime(pool, BuilderRuntimeConfig{
		URL:          os.Getenv(EnvBuilderURL),
		SharedSecret: os.Getenv(EnvBuilderSharedSecret),
		Concurrency:  agent.WorkerConcurrencyFromEnv(),
		Logger:       logger,
	})
}

// NewBuilderRuntime wires the builder. It returns (nil, nil) when cfg.URL is
// empty. A set URL with a missing or too short secret is an error: the caller
// should fail start-up with it (the message never contains the secret).
func NewBuilderRuntime(pool *db.Pool, cfg BuilderRuntimeConfig) (*BuilderRuntime, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, nil
	}
	if pool == nil {
		return nil, errors.New("builder: database pool is required")
	}
	signer, err := builder.NewSigner([]byte(cfg.SharedSecret))
	if err != nil {
		return nil, fmt.Errorf("builder: %s must be set to a secret of at least %d bytes when %s is set: %w",
			EnvBuilderSharedSecret, builder.MinSecretLen, EnvBuilderURL, err)
	}
	raw, err := sandbox.NewHTTP(cfg.URL, signer, cfg.HTTPClient)
	if err != nil {
		return nil, fmt.Errorf("builder: invalid %s: %w", EnvBuilderURL, err)
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	st := store.New(pool)
	sb := sandbox.NewRestoring(raw, agent.SnapshotProvider(st))

	reg := tools.NewRegistry(tools.WithLogger(log))
	if err := tools.RegisterWorkspaceTools(reg, sb, tools.WorkspaceConfig{PreviewBase: tools.DefaultPreviewBase}); err != nil {
		return nil, fmt.Errorf("builder: register workspace tools: %w", err)
	}
	if err := RegisterBuilderTools(reg, BuilderToolDeps{Pool: pool, Sessions: st}); err != nil {
		return nil, fmt.Errorf("builder: register orbit tools: %w", err)
	}

	rt := &BuilderRuntime{
		pool: pool, store: st, sandbox: sb, signer: signer, log: log,
		providerHTTP: newAIProviderHTTPClient(nil),
	}

	deps := agent.Deps{
		Store:    st,
		Provider: unavailableProvider{},
		Tools:    reg,
		Builder:  sb,
		Budget:   store.NewBudgetChecker(pool),
		Prompt:   rt.promptSource,
		// The same function the workspace tools were registered with: the
		// revert turn's rebuild must use the base the preview is served under.
		PreviewBase: tools.DefaultPreviewBase,
		Logger:      log,
	}
	concurrency := cfg.Concurrency
	if concurrency < 1 {
		concurrency = agent.WorkerConcurrencyFromEnv()
	}
	w, err := agent.NewWorker(deps, st, agent.WorkerConfig{
		Concurrency: concurrency,
		PrepareDeps: rt.prepareDeps,
	})
	if err != nil {
		return nil, fmt.Errorf("builder: create worker: %w", err)
	}
	rt.worker = w
	return rt, nil
}

// HandlerConfig is the part of BuilderHandlerConfig that comes from the
// runtime. On a nil runtime it is the zero config: untyped-nil Sandbox, no
// store, no signer, no Poke (every builder route but status is then a 503).
// The caller fills Logger and Dashboard.
func (rt *BuilderRuntime) HandlerConfig() BuilderHandlerConfig {
	if rt == nil {
		return BuilderHandlerConfig{}
	}
	return BuilderHandlerConfig{
		Store:         rt.store,
		Sandbox:       rt.sandbox,
		PreviewSigner: rt.signer,
		Poke:          rt.worker.Poke,
	}
}

// Start runs the turn worker in its own goroutine until ctx is cancelled or
// Stop is called. It is a no-op on a nil runtime and on a second call.
func (rt *BuilderRuntime) Start(ctx context.Context) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.done != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel
	rt.done = make(chan struct{})
	done := rt.done
	go func() {
		defer close(done)
		if err := rt.worker.Run(runCtx); err != nil {
			rt.log.Error("builder worker stopped with an error", "err", err)
		}
	}()
}

// Stop stops claiming turns and waits for the in-flight ones, up to the
// worker's ShutdownGrace (30s; turns still running after that are released to
// another replica) or ctx, whichever ends first. Safe on a nil or never
// started runtime.
func (rt *BuilderRuntime) Stop(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	cancel, done := rt.cancel, rt.done
	rt.mu.Unlock()
	if done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---- per-turn resolution ----

// prepareDeps is agent.WorkerConfig.PrepareDeps: limits and provider from the
// settings tables, read fresh for every turn.
func (rt *BuilderRuntime) prepareDeps(ctx context.Context, base agent.Deps) agent.Deps {
	s, err := GetBuilderSettings(ctx, rt.pool)
	if err != nil {
		rt.log.Error("builder: read settings for a turn failed", "err", err)
		base.Provider = unavailableProvider{}
		return base
	}
	base.Limits = agent.Limits{
		MaxSteps:            s.MaxStepsPerTurn,
		MaxFixIterations:    s.MaxFixIterations,
		MaxTurnDuration:     time.Duration(s.MaxTurnSeconds) * time.Second,
		ContextWindowTokens: s.ContextWindowTokens,
	}
	base.Provider = rt.resolveProvider(ctx, s.Provider)
	return base
}

// resolveProvider builds the model client for the provider named in the
// settings. Any failure yields an unavailableProvider (the turn then ends with
// provider_unavailable); the cause is logged without the key.
func (rt *BuilderRuntime) resolveProvider(ctx context.Context, name string) provider.Provider {
	prt, err := ResolveAIProviderRuntime(ctx, rt.pool, name)
	if err != nil {
		rt.log.Warn("builder: model provider is not available", "provider", name, "err", err)
		return unavailableProvider{}
	}
	p, err := provider.NewOpenAI(provider.OpenAIConfig{
		Name:       prt.Provider,
		BaseURL:    prt.BaseURL,
		APIKey:     prt.APIKey,
		Model:      prt.Model,
		HTTPClient: rt.providerHTTP,
	})
	if err != nil {
		rt.log.Warn("builder: model provider could not be built", "provider", name, "err", err)
		return unavailableProvider{}
	}
	return p
}

// unavailableProvider is the Provider of a turn whose real provider could not
// be resolved. Its Chat error is an *provider.ErrProvider, which the loop maps
// to the generic provider_unavailable code.
type unavailableProvider struct{}

func (unavailableProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, &provider.ErrProvider{Err: errors.New(providerUnavailMsg)}
}
func (unavailableProvider) Name() string  { return "unavailable" }
func (unavailableProvider) Model() string { return "" }

// ---- prompt source ----

// promptSource is agent.PromptSource. The company layer is the prompt version
// pinned when the session was created (later edits do not change a running
// session); a database failure fails the turn rather than silently dropping
// the company rules. The workspace-derived parts (template guide, file tree)
// and the app name degrade to empty: the agent has tools to look at all three.
func (rt *BuilderRuntime) promptSource(ctx context.Context, s *store.Session) (agent.PromptInput, error) {
	in := agent.PromptInput{Session: agent.SessionContext{
		BackendAppID: s.BackendAppID,
		Locale:       s.Locale,
	}}

	if s.PromptVersionID != nil {
		v, err := GetBuilderPromptVersion(ctx, rt.pool, *s.PromptVersionID)
		if err != nil {
			return agent.PromptInput{}, err
		}
		if v != nil {
			in.Company = agent.CompanyLayer(v.Persona, v.Rules)
		}
	}

	var name string
	if err := rt.pool.QueryRow(ctx, `SELECT name FROM zeep_system.apps WHERE id = $1`, s.BackendAppID).Scan(&name); err != nil {
		rt.log.Warn("builder: backend app name unavailable for the prompt", "session", s.ID)
	} else {
		in.Session.BackendAppName = name
	}

	sctx, cancel := context.WithTimeout(ctx, builderPromptScanTimeout)
	defer cancel()
	guide, tree, err := rt.scanWorkspace(sctx, s.ID)
	if err != nil {
		if ctx.Err() != nil {
			return agent.PromptInput{}, ctx.Err()
		}
		rt.log.Warn("builder: workspace scan for the prompt failed", "session", s.ID, "err", err)
	}
	in.TemplateGuide = guide
	in.Session.FileTree = tree
	return in, nil
}

// scanWorkspace reads the session's committed tree (HEAD) from the builder and
// extracts the template guide (ORBIT_AGENT.md at the root, the default
// template ships one) and a depth-limited file listing. On a read error it
// returns whatever was gathered before it.
func (rt *BuilderRuntime) scanWorkspace(ctx context.Context, sessionID string) (guide, tree string, err error) {
	rc, err := rt.sandbox.Archive(ctx, sessionID, "")
	if err != nil {
		return "", "", err
	}
	defer rc.Close()
	return scanWorkspaceArchive(rc)
}

func scanWorkspaceArchive(r io.Reader) (guide, tree string, err error) {
	var (
		paths []string
		seen  = map[string]bool{}
	)
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	tr := tar.NewReader(r)
	for n := 0; n < maxScanEntries; n++ {
		hdr, nerr := tr.Next()
		if errors.Is(nerr, io.EOF) {
			break
		}
		if nerr != nil {
			err = nerr
			break
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := cleanTreePath(hdr.Name)
		if !ok || treeExcluded(name) {
			continue
		}
		if name == templateGuideFile && guide == "" {
			b, rerr := io.ReadAll(io.LimitReader(tr, maxGuideBytes))
			if rerr != nil {
				err = rerr
				break
			}
			if utf8.Valid(b) {
				guide = string(b)
			} else {
				guide = strings.ToValidUTF8(string(b), "")
			}
		}
		segs := strings.Split(name, "/")
		if len(segs) > maxTreeDepth {
			add(strings.Join(segs[:maxTreeDepth], "/") + "/")
		} else {
			add(name)
		}
	}
	sort.Strings(paths)
	if len(paths) > maxTreeLines {
		paths = append(paths[:maxTreeLines], "... (more files not listed)")
	}
	return guide, strings.Join(paths, "\n"), err
}
