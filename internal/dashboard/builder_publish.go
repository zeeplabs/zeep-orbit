package dashboard

// builder_publish.go — publish a builder session as a frontend app, and the ZIP
// download fallback (ai-frontend-builder T32; AFB-22, design §12). Methods of
// *BuilderHandler, mounted under /dashboard behind RequireAuth (Publish also
// behind the AI chat limiter: it spends GitHub and Render API calls):
//
//	POST /api/builder/sessions/{id}/publish        Publish      owner; republish also CanWrite on the frontend app
//	GET  /api/builder/sessions/{id}/download.zip   DownloadZip  owner
//
// First publish: preconditions -> GitHub repo -> one commit with the tree of the
// session HEAD -> frontend app record linked to the backend app -> deploy key
// (setupSync) -> Render service (attemptDeploy). Republish: one more commit on
// top of the last published one. GitHub history gets one commit per publish.
//
// Preconditions are all checked before the first side effect and answer
// 409 {"error": ..., "reason": ...}: github_not_configured,
// deploy_not_configured, template_not_deployable, template_unavailable,
// nothing_to_publish, turn_in_progress, publish_in_progress. A branch that moved
// outside the builder is 409 reason remote_moved and nothing is pushed (the
// remote is never overwritten).
//
// Failure semantics: a failure before the code is on GitHub leaves no frontend
// app behind; once it is there the frontend app exists and the session records
// it. A Render failure does NOT undo that: the frontend app keeps status "ready"
// (the repository is fine) with deploy_status "failed", exactly like the existing
// create flow, and the existing deploy retry applies. The session is never
// modified by a failed publish.

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/agent/sandbox"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/deploy"
	"github.com/zeeplabs/zeep-orbit/internal/github"
)

// DeployProviderFactory builds the deploy provider publish uses. nil selects the
// Render provider (buildDeployProvider); tests inject a fake.
type DeployProviderFactory func(ctx context.Context, cfg *DeployProviderConfig) (deploy.DeployProvider, error)

// Reasons of a 409 from the publish route.
const (
	PublishReasonGitHubNotConfigured = "github_not_configured"
	PublishReasonDeployNotConfigured = "deploy_not_configured"
	PublishReasonTemplateNotDeploy   = "template_not_deployable"
	PublishReasonTemplateUnavailable = "template_unavailable"
	PublishReasonNothingToPublish    = "nothing_to_publish"
	PublishReasonTurnInProgress      = "turn_in_progress"
	PublishReasonInProgress          = "publish_in_progress"
	PublishReasonRemoteMoved         = "remote_moved"
)

const (
	// builderPublishBranch is the branch every publish writes to.
	builderPublishBranch = "main"

	builderPublishBodyLimit = 4 << 10
	// builderPublishTimeout bounds the whole orchestration (GitHub, Render),
	// which runs detached from the client connection so a closed tab cannot
	// leave a half-published project.
	builderPublishTimeout = 5 * time.Minute

	// Limits of the tree read from the builder. They sit under the GitHub
	// writer's own limits so the failure is a clear 422, not a GitHub error.
	maxPublishFiles     = 4000
	maxPublishTreeBytes = 80 << 20
	maxPublishFileBytes = 20 << 20
	// maxZipTreeBytes bounds the uncompressed size of the ZIP download.
	maxZipTreeBytes = 256 << 20

	// The default template has no github_templates row of its own, but a
	// frontend app needs one (frontend_apps.template_id is NOT NULL). publish
	// keeps one hidden, inactive row for it, found by these coordinates.
	builderDefaultTemplateOwner = "orbit-builder"
	builderDefaultTemplateRepo  = "default-template"
	builderDefaultTemplateBy    = "system"

	maxPublishSlugLen = 50
)

var publishSubdomainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type publishBuilderRequest struct {
	// Subdomain is optional: with a base domain configured on the deploy
	// provider it becomes <subdomain>.<base domain>.
	Subdomain string `json:"subdomain"`
}

// BuilderPublishResult is the body of the publish route.
type BuilderPublishResult struct {
	// FrontendApp is the published app (re-read after the deploy step, so its
	// deploy_status/deploy_error_message are current).
	FrontendApp *FrontendApp `json:"frontend_app"`
	// PublishedSHA is the GitHub commit this publish pushed.
	PublishedSHA string `json:"published_sha"`
	// Republish is true when an existing frontend app was updated.
	Republish bool `json:"republish"`
}

func (h *BuilderHandler) writePublishConflict(w http.ResponseWriter, reason, msg string) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": msg, "reason": reason})
}

// publishEvent records a publish_* event; a failure to record it never fails the
// publish.
func (h *BuilderHandler) publishEvent(ctx context.Context, sessionID, eventType string, payload any) {
	if _, err := h.store.AppendSessionEvent(ctx, sessionID, "", eventType, payload); err != nil {
		h.logger.Warn("builder publish: record event", zap.String("event", eventType), zap.Error(err))
	}
}

// Publish handles POST /dashboard/api/builder/sessions/{id}/publish.
func (h *BuilderHandler) Publish(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	var req publishBuilderRequest
	if r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, builderPublishBodyLimit)
		if err := decodeOptionalJSON(r.Body, &req); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	req.Subdomain = strings.ToLower(strings.TrimSpace(req.Subdomain))
	if req.Subdomain != "" && !publishSubdomainPattern.MatchString(req.Subdomain) {
		h.writeError(w, http.StatusBadRequest, "invalid subdomain")
		return
	}

	// ---- preconditions: no side effect before all of them hold ----
	switch {
	case sess.HeadSHA == "":
		h.writePublishConflict(w, PublishReasonNothingToPublish, "the project has nothing to publish yet")
		return
	case sess.Status == store.SessionRunning, sess.Status == store.SessionQueued, sess.Status == store.SessionAwaitingConfirmation:
		h.writePublishConflict(w, PublishReasonTurnInProgress, "wait for the current step to finish before publishing")
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), builderPublishTimeout)
	defer cancel()

	ghCfg, err := GetGitHubConfig(ctx, h.pool)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if ghCfg == nil || ghCfg.InstallationID == nil {
		h.writePublishConflict(w, PublishReasonGitHubNotConfigured, "GitHub is not connected: connect it in the integrations settings, or download the project as a ZIP")
		return
	}
	client, err := h.githubClient(ctx)
	if err != nil || client == nil {
		if err == nil {
			err = errors.New("github client unavailable")
		}
		h.serverError(w, r, err)
		return
	}

	app, err := h.linkedFrontendApp(ctx, sess)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	republish := app != nil
	var tpl *GitHubTemplate
	if republish {
		role, rerr := ResolveAppRole(ctx, h.pool, user, AppRef{FrontendAppID: app.ID})
		if rerr != nil {
			h.serverError(w, r, rerr)
			return
		}
		if !role.CanWrite() {
			h.writeError(w, http.StatusForbidden, builderForbiddenMsg)
			return
		}
	} else {
		dcfg, derr := GetDeployProviderConfig(ctx, h.pool)
		if derr != nil {
			h.serverError(w, r, derr)
			return
		}
		if dcfg == nil {
			h.writePublishConflict(w, PublishReasonDeployNotConfigured, "no deploy provider is connected: connect one in the integrations settings, or download the project as a ZIP")
			return
		}
		var ok bool
		if tpl, ok = h.publishTemplate(ctx, w, r, sess); !ok {
			return
		}
		if ghCfg.OrgLogin == "" {
			h.writePublishConflict(w, PublishReasonGitHubNotConfigured, "the connected GitHub installation has no organization")
			return
		}
	}

	// One publish per session at a time (a second tab, a double click): a
	// transaction-scoped advisory lock, held for the whole publish, costs no
	// row lock on the session.
	lockTx, err := h.pool.Begin(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	defer lockTx.Rollback(ctx) //nolint:errcheck
	var locked bool
	if err := lockTx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, "builder-publish:"+sess.ID).Scan(&locked); err != nil {
		h.serverError(w, r, err)
		return
	}
	if !locked {
		h.writePublishConflict(w, PublishReasonInProgress, "a publish of this project is already running")
		return
	}

	// ---- the tree: read before any side effect, so a builder failure is clean ----
	files, err := h.readPublishTree(ctx, sess.ID)
	if err != nil {
		h.publishFailure(w, r, sess, "archive_failed", err)
		return
	}
	h.publishEvent(ctx, sess.ID, "publish_started", map[string]any{"republish": republish})
	message := fmt.Sprintf("Publish from Orbit AI Builder (turn %d)", sess.HeadTurnSeq)

	fa := &FrontendAppsHandler{pool: h.pool, httpClient: h.githubHTTP, newDeployProvider: h.deployFactory}

	if republish {
		h.republish(ctx, w, r, user, sess, app, client, files, message)
		return
	}

	// ---- first publish ----
	slug, err := h.freeSlug(ctx, sess.Title)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	owner := ghCfg.OrgLogin
	repoInfo, err := createPublishRepo(ctx, client, owner, slug)
	if err != nil {
		h.publishFailure(w, r, sess, "github_failed", err)
		return
	}
	sha, err := client.CommitTree(ctx, owner, repoInfo.Name, builderPublishBranch, files, message, "")
	if err != nil {
		// Nothing references the new repository yet: archive it instead of
		// leaving an empty one behind (best effort).
		if aerr := client.ArchiveRepo(ctx, owner, repoInfo.Name); aerr != nil {
			h.logger.Warn("builder publish: archive repository after a failed first commit",
				zap.String("repo", repoInfo.Name), zap.Error(aerr))
		}
		h.publishPushFailure(w, r, sess, err)
		return
	}

	if tpl.ID == "" {
		// The default template: a frontend app needs a template row, so the
		// hidden one is created (or refreshed) now that the code is pushed.
		if tpl, err = ensureBuilderDefaultTemplate(ctx, h.pool); err != nil {
			h.publishFailure(w, r, sess, "internal", err)
			return
		}
	}
	created, err := CreateFrontendApp(ctx, h.pool, FrontendAppInput{
		Name:          sess.Title,
		Slug:          slug,
		TemplateID:    tpl.ID,
		GithubRepoURL: repoInfo.HTMLURL,
		Status:        "ready",
		CreatedBy:     user.Email,
		OwnerID:       user.ID,
		BackendAppID:  sess.BackendAppID,
	})
	if err != nil {
		h.publishFailure(w, r, sess, "internal", err)
		return
	}
	// The publisher administers the app they created (the existing create flow
	// relies on a migration for this; a new app would otherwise be invisible to
	// a non-superadmin owner).
	if _, merr := AddAppMember(ctx, h.pool, AppRef{FrontendAppID: created.ID}, user.ID, AppRoleAdmin); merr != nil && !errors.Is(merr, ErrAlreadyMember) {
		h.logger.Warn("builder publish: add owner as frontend app admin", zap.String("frontend_app_id", created.ID), zap.Error(merr))
	}
	if err := h.store.RecordPublish(ctx, user.ID, sess.ID, &created.ID, sha); err != nil {
		h.publishFailure(w, r, sess, "internal", err)
		return
	}

	if cerr := CreateSyncCredential(ctx, h.pool, created.ID); cerr == nil {
		fa.setupSync(ctx, client, created)
	} else {
		h.logger.Warn("builder publish: create sync credential", zap.String("frontend_app_id", created.ID), zap.Error(cerr))
	}
	fa.attemptDeploy(ctx, created, tpl, req.Subdomain)

	final, _, err := GetFrontendApp(ctx, h.pool, created.ID, user)
	if err != nil {
		final = created
	}
	h.publishEvent(ctx, sess.ID, "publish_succeeded", map[string]any{"republish": false, "sha": sha})
	h.audit(r.Context(), user, "builder.session.publish", sess.ID, sess.Title,
		map[string]any{"frontend_app_id": created.ID, "republish": false}, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, BuilderPublishResult{FrontendApp: final, PublishedSHA: sha, Republish: false})
}

// republish pushes one more commit on top of the last published one.
func (h *BuilderHandler) republish(ctx context.Context, w http.ResponseWriter, r *http.Request, user *DashboardUser,
	sess *store.Session, app *FrontendApp, client *github.Client, files []github.TreeFile, message string) {
	owner, repo := parseGitHubOwnerRepo(app.GithubRepoURL)
	if owner == "" || repo == "" {
		h.publishFailure(w, r, sess, "internal", errors.New("cannot parse owner/repo from the frontend app's repository URL"))
		return
	}
	sha, err := client.CommitTree(ctx, owner, repo, builderPublishBranch, files, message, sess.PublishedSHA)
	if err != nil {
		h.publishPushFailure(w, r, sess, err)
		return
	}
	if err := h.store.RecordPublish(ctx, user.ID, sess.ID, nil, sha); err != nil {
		// The code is on GitHub but the next republish would be refused as
		// "moved": surface it instead of pretending all is well.
		h.publishFailure(w, r, sess, "internal", err)
		return
	}
	h.publishEvent(ctx, sess.ID, "publish_succeeded", map[string]any{"republish": true, "sha": sha})
	h.audit(r.Context(), user, "builder.session.publish", sess.ID, sess.Title,
		map[string]any{"frontend_app_id": app.ID, "republish": true}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, BuilderPublishResult{FrontendApp: app, PublishedSHA: sha, Republish: true})
}

// publishPushFailure answers a failed CommitTree: a moved remote is a 409 with
// reason remote_moved (nothing was pushed), anything else a generic 502.
func (h *BuilderHandler) publishPushFailure(w http.ResponseWriter, r *http.Request, sess *store.Session, err error) {
	if errors.Is(err, github.ErrRemoteMoved) {
		h.publishEvent(context.WithoutCancel(r.Context()), sess.ID, "publish_failed", map[string]any{"code": PublishReasonRemoteMoved})
		h.writePublishConflict(w, PublishReasonRemoteMoved, "repository changed outside the builder")
		return
	}
	h.publishFailure(w, r, sess, "github_failed", err)
}

// publishFailure records publish_failed{code}, logs err and answers a fixed
// error: 503 when the builder could not give the tree, 502 for GitHub, 500
// otherwise. err never reaches the response.
func (h *BuilderHandler) publishFailure(w http.ResponseWriter, r *http.Request, sess *store.Session, code string, err error) {
	h.publishEvent(context.WithoutCancel(r.Context()), sess.ID, "publish_failed", map[string]any{"code": code})
	var tooLarge *publishTreeError
	switch {
	case errors.As(err, &tooLarge):
		h.logger.Info("builder publish: project cannot be published", zap.String("session_id", sess.ID), zap.String("why", tooLarge.msg))
		h.writeError(w, http.StatusUnprocessableEntity, "the project is too large or has files that cannot be published")
	case code == "archive_failed":
		h.logger.Error("builder publish: read project tree", zap.String("session_id", sess.ID), zap.Error(err))
		h.writeError(w, http.StatusServiceUnavailable, builderUnavailableMsg)
	case code == "github_failed":
		h.logger.Error("builder publish: GitHub request failed", zap.String("session_id", sess.ID), zap.Error(err))
		h.writeError(w, http.StatusBadGateway, "GitHub request failed")
	default:
		h.serverError(w, r, err)
	}
}

// linkedFrontendApp returns the live (not archived) frontend app the session was
// published as, or nil when it was never published or the app is gone, in which
// case the next publish creates a new one.
func (h *BuilderHandler) linkedFrontendApp(ctx context.Context, sess *store.Session) (*FrontendApp, error) {
	if sess.FrontendAppID == nil {
		return nil, nil
	}
	var a FrontendApp
	err := scanApp(&a, h.pool.QueryRow(ctx,
		`SELECT fa.id, fa.name, fa.slug, fa.template_id,
		 COALESCE(gt.name, ''), fa.github_repo_url, fa.status,
		 fa.error_message, fa.created_by, fa.created_at, fa.archived_at,
		 `+faExtraColsSelect+`
		 FROM zeep_system.frontend_apps fa
		 LEFT JOIN zeep_system.github_templates gt ON gt.id = fa.template_id
		 LEFT JOIN zeep_system.dashboard_users u ON u.id = fa.owner_id
		 WHERE fa.id = $1 AND fa.archived_at IS NULL`, *sess.FrontendAppID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("dashboard: builder publish: load linked frontend app: %w", err)
	}
	return &a, nil
}

// publishTemplate resolves the template record the new frontend app is created
// from and checks it can be deployed: it needs a Render service type, a build
// command and (static site) a publish path or (web service) a start command.
// Builder-enabled templates are not validated when enabled, so this is where an
// unusable one is caught. It answers the 409 itself (ok=false).
func (h *BuilderHandler) publishTemplate(ctx context.Context, w http.ResponseWriter, r *http.Request, sess *store.Session) (*GitHubTemplate, bool) {
	var (
		tpl *GitHubTemplate
		err error
	)
	if sess.TemplateKind == store.TemplateDefault || sess.TemplateID == nil {
		// Nothing is written until the code is on GitHub (see
		// ensureBuilderDefaultTemplate in Publish).
		tpl = builtinDefaultTemplateRow()
	} else {
		tpl, err = getGitHubTemplateByID(ctx, h.pool, *sess.TemplateID)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.writePublishConflict(w, PublishReasonTemplateUnavailable, "the project's template is no longer available")
			return nil, false
		}
		h.serverError(w, r, err)
		return nil, false
	}
	var missing []string
	if strings.TrimSpace(tpl.RenderServiceType) == "" {
		missing = append(missing, "render_service_type")
	}
	if strings.TrimSpace(tpl.BuildCommand) == "" {
		missing = append(missing, "build_command")
	}
	switch tpl.RenderServiceType {
	case "web_service":
		if strings.TrimSpace(tpl.StartCommand) == "" {
			missing = append(missing, "start_command")
		}
	default:
		if strings.TrimSpace(tpl.PublishPath) == "" {
			missing = append(missing, "publish_path")
		}
	}
	if len(missing) > 0 {
		h.writePublishConflict(w, PublishReasonTemplateNotDeploy,
			"the project's template cannot be deployed: it is missing "+strings.Join(missing, ", "))
		return nil, false
	}
	return tpl, true
}

// getGitHubTemplateByID loads one template row (active or not).
func getGitHubTemplateByID(ctx context.Context, pool *db.Pool, id string) (*GitHubTemplate, error) {
	if !looksLikeUUID(id) {
		return nil, ErrNotFound
	}
	var t GitHubTemplate
	err := scanTemplate(&t, pool.QueryRow(ctx,
		`SELECT `+githubTemplateCols+` FROM zeep_system.github_templates WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get github template: %w", err)
	}
	return &t, nil
}

// builtinDefaultTemplateRow is the default template's deploy metadata, from the
// built-in constants, without a database row (ID is empty).
func builtinDefaultTemplateRow() *GitHubTemplate {
	return &GitHubTemplate{
		Name: builderDefaultTemplateName, Framework: builderDefaultTemplateFramework,
		GitHubOwner: builderDefaultTemplateOwner, GitHubRepo: builderDefaultTemplateRepo,
		RenderServiceType: builderDefaultTemplateRenderType, BuildCommand: builderDefaultTemplateBuildCmd,
		PublishPath: builderDefaultTemplatePublishPath,
	}
}

// ensureBuilderDefaultTemplate returns the hidden template row that stands for
// the builder's built-in default template (a frontend app needs a template row),
// creating it on first use and refreshing its deploy metadata from the built-in
// constants each time. The row is inactive and not builder_enabled, so neither
// the regular frontend-app creation nor the builder template picker offers it.
func ensureBuilderDefaultTemplate(ctx context.Context, pool *db.Pool) (*GitHubTemplate, error) {
	var t GitHubTemplate
	err := scanTemplate(&t, pool.QueryRow(ctx,
		`UPDATE zeep_system.github_templates
		 SET render_service_type = $4, build_command = $5, publish_path = $6, framework = $7
		 WHERE id = (SELECT id FROM zeep_system.github_templates
		             WHERE github_owner = $1 AND github_repo = $2 AND created_by = $3
		             ORDER BY created_at LIMIT 1)
		 RETURNING `+githubTemplateCols,
		builderDefaultTemplateOwner, builderDefaultTemplateRepo, builderDefaultTemplateBy,
		builderDefaultTemplateRenderType, builderDefaultTemplateBuildCmd, builderDefaultTemplatePublishPath,
		builderDefaultTemplateFramework))
	if err == nil {
		return &t, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("dashboard: ensure builder default template: %w", err)
	}
	err = scanTemplate(&t, pool.QueryRow(ctx,
		`INSERT INTO zeep_system.github_templates
		 (name, description, github_owner, github_repo, framework, active, created_by,
		  render_service_type, build_command, publish_path, start_command)
		 VALUES ($1, $2, $3, $4, $5, false, $6, $7, $8, $9, '')
		 RETURNING `+githubTemplateCols,
		builderDefaultTemplateName, "Built-in template of the AI Frontend Builder. Not a repository.",
		builderDefaultTemplateOwner, builderDefaultTemplateRepo, builderDefaultTemplateFramework, builderDefaultTemplateBy,
		builderDefaultTemplateRenderType, builderDefaultTemplateBuildCmd, builderDefaultTemplatePublishPath))
	if err != nil {
		return nil, fmt.Errorf("dashboard: create builder default template: %w", err)
	}
	return &t, nil
}

// freeSlug derives a unique frontend app slug from the session title.
func (h *BuilderHandler) freeSlug(ctx context.Context, title string) (string, error) {
	base := slugify(title)
	if len(base) > maxPublishSlugLen {
		base = strings.Trim(base[:maxPublishSlugLen], "-")
	}
	if base == "" {
		base = "project"
	}
	cand := base
	for i := 0; i < 6; i++ {
		exists, err := SlugExists(ctx, h.pool, cand)
		if err != nil {
			return "", err
		}
		if !exists {
			return cand, nil
		}
		cand = base + "-" + randomSuffix()
	}
	return "", errors.New("could not find a free slug")
}

func randomSuffix() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "x" + fmt.Sprint(time.Now().UnixNano()%100000)
	}
	return hex.EncodeToString(b)
}

// createPublishRepo creates the empty repository, retrying with a random suffix
// when the name is taken on GitHub (the slug is only unique among Orbit's apps).
func createPublishRepo(ctx context.Context, client *github.Client, owner, name string) (github.RepoInfo, error) {
	cand := name
	for i := 0; i < 4; i++ {
		info, err := client.CreateEmptyRepo(ctx, owner, cand, true)
		if err == nil {
			return info, nil
		}
		if !errors.Is(err, github.ErrRepoExists) {
			return github.RepoInfo{}, err
		}
		cand = name + "-" + randomSuffix()
	}
	return github.RepoInfo{}, github.ErrRepoExists
}

// ---- reading the tree ----

// publishTreeError marks a tree that cannot be published (too big, too many
// files, an entry that is not a regular file). The text is for logs only.
type publishTreeError struct{ msg string }

func (e *publishTreeError) Error() string { return "builder publish: " + e.msg }

// treeExcluded reports whether an archive path never leaves the builder: version
// control data, installed dependencies, build output and the builder's own
// bookkeeping, at any depth.
func treeExcluded(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		switch seg {
		case ".git", "node_modules", "dist", ".orbit":
			return true
		}
	}
	return false
}

// cleanTreePath normalizes a tar entry name; ok=false for an unsafe one
// (absolute, or climbing out with "..").
func cleanTreePath(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\x00\\") {
		return "", false
	}
	c := path.Clean(name)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", false
	}
	return c, true
}

// readPublishTree reads the tree at the session HEAD from the builder.
func (h *BuilderHandler) readPublishTree(ctx context.Context, sessionID string) ([]github.TreeFile, error) {
	rc, err := h.sandbox.Archive(ctx, sessionID, "")
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var (
		files []github.TreeFile
		total int64
	)
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, &publishTreeError{msg: fmt.Sprintf("unsupported entry type %q", hdr.Typeflag)}
		}
		name, ok := cleanTreePath(hdr.Name)
		if !ok {
			return nil, &publishTreeError{msg: "unsafe path in the archive"}
		}
		if treeExcluded(name) {
			continue
		}
		if hdr.Size > maxPublishFileBytes {
			return nil, &publishTreeError{msg: "a file is too large"}
		}
		total += hdr.Size
		if total > maxPublishTreeBytes || len(files) >= maxPublishFiles {
			return nil, &publishTreeError{msg: "the tree is too large"}
		}
		content, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
		if err != nil {
			return nil, fmt.Errorf("read archive entry: %w", err)
		}
		files = append(files, github.TreeFile{Path: name, Content: content, Executable: hdr.Mode&0o111 != 0})
	}
	if len(files) == 0 {
		return nil, &publishTreeError{msg: "the tree is empty"}
	}
	return files, nil
}

// ---- ZIP download ----

// DownloadZip handles GET /dashboard/api/builder/sessions/{id}/download.zip: the
// session HEAD as a ZIP, for owners who cannot or do not want to publish. It
// never includes node_modules, .git, dist or the builder's bookkeeping.
func (h *BuilderHandler) DownloadZip(w http.ResponseWriter, r *http.Request) {
	user, sess, ok := h.ownedSession(w, r, false)
	if !ok {
		return
	}
	if sess.HeadSHA == "" {
		h.writeError(w, http.StatusConflict, "the project has nothing to download yet")
		return
	}
	rc, err := h.sandbox.Archive(r.Context(), sess.ID, "")
	if err != nil {
		var se *sandbox.Error
		if errors.As(err, &se) || errors.Is(err, sandbox.ErrUnknownSession) || errors.Is(err, sandbox.ErrWorkspaceLost) {
			h.logger.Error("builder download: archive", zap.String("session_id", sess.ID), zap.Error(err))
			h.writeError(w, http.StatusServiceUnavailable, builderUnavailableMsg)
			return
		}
		h.serverError(w, r, err)
		return
	}
	defer rc.Close()

	name := slugify(sess.Title)
	if name == "" {
		name = "project"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	if err := writeTreeZip(zw, rc); err != nil {
		// Headers are gone: all that is left is to log and cut the stream, so
		// the client sees a broken archive instead of a silently partial one.
		h.logger.Error("builder download: write zip", zap.String("session_id", sess.ID), zap.Error(err))
		return
	}
	if err := zw.Close(); err != nil {
		h.logger.Warn("builder download: finish zip", zap.String("session_id", sess.ID), zap.Error(err))
		return
	}
	h.audit(r.Context(), user, "builder.session.download", sess.ID, sess.Title, nil, r.RemoteAddr)
}

// writeTreeZip copies the regular files of a tar into zw, skipping excluded
// paths (treeExcluded) and refusing unsafe names and oversized trees.
func writeTreeZip(zw *zip.Writer, tarStream io.Reader) error {
	var total int64
	tr := tar.NewReader(tarStream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // pax headers, directories (symlinks never reach a ZIP)
		}
		name, ok := cleanTreePath(hdr.Name)
		if !ok {
			return &publishTreeError{msg: "unsafe path in the archive"}
		}
		if treeExcluded(name) {
			continue
		}
		total += hdr.Size
		if total > maxZipTreeBytes {
			return &publishTreeError{msg: "the tree is too large"}
		}
		fh := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: hdr.ModTime}
		fh.SetMode(hdr.FileInfo().Mode().Perm())
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, io.LimitReader(tr, hdr.Size)); err != nil {
			return err
		}
	}
}

// decodeOptionalJSON decodes a small JSON body, treating an empty one as {}.
func decodeOptionalJSON(r io.Reader, dst any) error {
	if err := json.NewDecoder(r).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
