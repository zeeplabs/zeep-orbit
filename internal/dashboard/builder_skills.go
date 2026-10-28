package dashboard

// builder_skills.go: HTTP handlers for the AI Builder's skills
// (ai-builder-skills S07). Every route is superadmin-only (the same gate as the
// builder settings) and mounted under /dashboard:
//
//	GET    /api/builder/skills           ListBuilderSkillsConfig
//	POST   /api/builder/skills           CreateBuilderSkillConfig
//	GET    /api/builder/skills/{name}    GetBuilderSkillConfig
//	PATCH  /api/builder/skills/{name}    UpdateBuilderSkillConfig
//	DELETE /api/builder/skills/{name}    DeleteBuilderSkillConfig
//
// The server owns the built-in lock: a built-in can only be enabled or
// disabled, whatever the client sends. Skill text is company-authored and is
// sent to the LLM provider, so it is never logged and the audit rows record
// name, version, hash and size only.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/zeeplabs/zeep-orbit/internal/agent/skills"
)

// builderSkillBodyLimit bounds the raw POST. The file itself is capped at
// skills.MaxContentBytes by the parser; this limit only leaves room for JSON
// escaping so a legitimate file is never cut off by the transport.
const builderSkillBodyLimit = 3 * skills.MaxContentBytes

// Machine reasons of the skills API (the UI maps them to localized text).
const (
	skillReasonInvalid       = "skill_invalid"
	skillReasonBuiltin       = "skill_builtin_readonly"
	skillReasonReserved      = "skill_name_reserved"
	skillReasonNotFound      = "skill_not_found"
	skillReasonIndexBudget   = "skills_index_budget_exceeded"
	skillReasonInvalidUpdate = "skill_update_invalid"
)

// writeSkillError writes {"error", "reason", ...extra} with an English message.
func writeSkillError(w http.ResponseWriter, status int, msg, reason string, extra map[string]any) {
	body := map[string]any{"error": msg, "reason": reason}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

func writeSkillBudgetError(w http.ResponseWriter, be *BuilderSkillsBudgetError) {
	writeSkillError(w, http.StatusConflict, "the skills index would exceed its size limit", skillReasonIndexBudget,
		map[string]any{"index": map[string]int{"chars": be.Chars, "max": be.Max}})
}

func skillSummaryByName(list []BuilderSkillSummary, name string) *BuilderSkillSummary {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// ListBuilderSkillsConfig handles GET /dashboard/api/builder/skills: built-ins
// first, then custom skills, and the index size against its budget.
func (h *Handler) ListBuilderSkillsConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	list, info, err := ListBuilderSkills(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": list, "index": info})
}

// GetBuilderSkillConfig handles GET /dashboard/api/builder/skills/{name}
// with an optional ?version=N: the skill with its body (read-only for
// built-ins) and, for a custom skill, its version history.
func (h *Handler) GetBuilderSkillConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	name := chi.URLParam(r, "name")
	version := 0
	if raw := r.URL.Query().Get("version"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeSkillError(w, http.StatusBadRequest, "invalid version", skillReasonInvalid, nil)
			return
		}
		version = n
	}
	d, err := GetBuilderSkill(r.Context(), h.pool, name, version)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if d == nil {
		writeSkillError(w, http.StatusNotFound, "skill not found", skillReasonNotFound, nil)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// CreateBuilderSkillConfig handles POST /dashboard/api/builder/skills with
// {"content": "<SKILL.md text>"}. It creates version 1 (201) or a new version
// of a live custom skill (200); identical content is a no-op (created=false).
// A built-in name is 409 skill_builtin_readonly, whatever the content.
func (h *Handler) CreateBuilderSkillConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, builderSkillBodyLimit)
	var body struct {
		Content string `json:"content"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	// Credentials first: such content is never parsed further or stored.
	if skills.DetectCredentials(body.Content) {
		writeSkillError(w, http.StatusBadRequest, "the file appears to contain a credential", skillReasonInvalid,
			map[string]any{"fields": []skills.FieldError{{Field: "content", Code: skills.CodeCredentialsDetected}}})
		return
	}

	parsed, warnings, err := skills.Parse([]byte(body.Content))
	if err != nil {
		if ve, isValidation := skills.AsValidationError(err); isValidation {
			writeSkillError(w, http.StatusBadRequest, "invalid skill file", skillReasonInvalid,
				map[string]any{"fields": ve.Fields, "warnings": nonNilWarnings(warnings)})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	res, err := SaveBuilderSkill(r.Context(), h.pool, user.ID, parsed)
	if err != nil {
		var be *BuilderSkillsBudgetError
		switch {
		case errors.Is(err, ErrBuilderSkillBuiltin):
			writeSkillError(w, http.StatusConflict, "built-in skills are read-only", skillReasonBuiltin, nil)
		case errors.Is(err, ErrBuilderSkillReserved):
			writeSkillError(w, http.StatusBadRequest, "the orbit- name prefix is reserved for built-in skills", skillReasonReserved,
				map[string]any{"fields": []skills.FieldError{{Field: "name", Code: skillReasonReserved}}})
		case errors.As(err, &be):
			writeSkillBudgetError(w, be)
		default:
			h.writeError(w, r, http.StatusInternalServerError, "failed to save skill", err)
		}
		return
	}

	list, info, err := ListBuilderSkills(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	status := http.StatusOK
	if res.Created && res.NewLineage {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"skill":    skillSummaryByName(list, res.Name),
		"created":  res.Created,
		"version":  res.Version,
		"warnings": nonNilWarnings(warnings),
		"index":    info,
	})

	if res.Created {
		action := "builder.skill.version"
		if res.NewLineage {
			action = "builder.skill.create"
		}
		meta, _ := json.Marshal(map[string]any{
			"name": res.Name, "version": res.Version, "sha256": res.SHA256, "chars": res.Chars,
		})
		h.audit(r.Context(), user.ID, user.Email, action, "builder_skill", res.Name, res.Name, meta, r.RemoteAddr)
	}
}

// UpdateBuilderSkillConfig handles PATCH /dashboard/api/builder/skills/{name}
// with {"enabled": bool}. It works on built-ins (the only thing that can
// change on them) and on custom skills. Enabling a skill that would overflow
// the index is 409 skills_index_budget_exceeded.
func (h *Handler) UpdateBuilderSkillConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		writeSkillError(w, http.StatusBadRequest, "enabled is required", skillReasonInvalidUpdate, nil)
		return
	}

	changed, err := SetBuilderSkillEnabled(r.Context(), h.pool, user.ID, name, *body.Enabled)
	if err != nil {
		var be *BuilderSkillsBudgetError
		switch {
		case errors.Is(err, ErrBuilderSkillNotFound):
			writeSkillError(w, http.StatusNotFound, "skill not found", skillReasonNotFound, nil)
		case errors.As(err, &be):
			writeSkillBudgetError(w, be)
		default:
			h.writeError(w, r, http.StatusInternalServerError, "failed to update skill", err)
		}
		return
	}

	list, info, err := ListBuilderSkills(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill":   skillSummaryByName(list, name),
		"changed": changed,
		"index":   info,
	})

	if changed {
		action := "builder.skill.disable"
		if *body.Enabled {
			action = "builder.skill.enable"
		}
		meta, _ := json.Marshal(map[string]any{"name": name, "enabled": *body.Enabled})
		h.audit(r.Context(), user.ID, user.Email, action, "builder_skill", name, name, meta, r.RemoteAddr)
	}
}

// DeleteBuilderSkillConfig handles DELETE /dashboard/api/builder/skills/{name}:
// a soft delete of a custom skill. A built-in is 409 skill_builtin_readonly.
func (h *Handler) DeleteBuilderSkillConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if err := DeleteBuilderSkill(r.Context(), h.pool, user.ID, name); err != nil {
		switch {
		case errors.Is(err, ErrBuilderSkillBuiltin):
			writeSkillError(w, http.StatusConflict, "built-in skills are read-only", skillReasonBuiltin, nil)
		case errors.Is(err, ErrBuilderSkillNotFound):
			writeSkillError(w, http.StatusNotFound, "skill not found", skillReasonNotFound, nil)
		default:
			h.writeError(w, r, http.StatusInternalServerError, "failed to delete skill", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})

	meta, _ := json.Marshal(map[string]any{"name": name})
	h.audit(r.Context(), user.ID, user.Email, "builder.skill.delete", "builder_skill", name, name, meta, r.RemoteAddr)
}

func nonNilWarnings(w []skills.Warning) []skills.Warning {
	if w == nil {
		return []skills.Warning{}
	}
	return w
}
