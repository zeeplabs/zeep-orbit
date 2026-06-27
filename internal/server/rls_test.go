package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/auth"
)

// buildRLSRouter monta um chi.Router com JWTMiddleware real (injeta AuthUser).
func buildRLSRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Route("/{app}/{table}", func(r chi.Router) {
		r.Use(JWTMiddleware(testReg))
		r.Get("/", h.HandleList)
		r.Post("/", h.HandleCreate)
	})
	r.Route("/{app}/{table}/{id}", func(r chi.Router) {
		r.Use(JWTMiddleware(testReg))
		r.Get("/", h.HandleGetByID)
		r.Put("/", h.HandleUpdate)
		r.Patch("/", h.HandleUpdate)
		r.Delete("/", h.HandleDelete)
	})
	return r
}

// insertRLSUser insere um usuário em _auth_users e retorna o UUID gerado.
func insertRLSUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := testPool.QueryRow(
		ctx,
		`INSERT INTO `+rlsSchema+`."_auth_users" (email, password_hash) VALUES ($1, 'x') RETURNING id`,
		email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertRLSUser %q: %v", email, err)
	}
	return id
}

// TestRLS cobre os acceptance criteria do ZC-21.
func TestRLS(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}

	h := NewHandler(testPool, testReg)
	router := buildRLSRouter(h)

	user1ID := insertRLSUser(t, "rls-user1@test.com")
	user2ID := insertRLSUser(t, "rls-user2@test.com")

	jwt1, err := auth.IssueJWT([]byte(rlsSecret), user1ID, "rls-user1@test.com", rlsAppName)
	if err != nil {
		t.Fatalf("IssueJWT user1: %v", err)
	}
	jwt2, err := auth.IssueJWT([]byte(rlsSecret), user2ID, "rls-user2@test.com", rlsAppName)
	if err != nil {
		t.Fatalf("IssueJWT user2: %v", err)
	}

	bearer1 := "Bearer " + jwt1
	bearer2 := "Bearer " + jwt2
	basePath := "/" + rlsAppName + "/notes"

	var noteID string

	t.Run("AC1_PostPopulatesOwnerID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, basePath+"/", jsonBody(map[string]any{"title": "nota do user1"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("esperado 201, obtido %d: %s", rec.Code, rec.Body.String())
		}
		var row map[string]any
		json.NewDecoder(rec.Body).Decode(&row)
		id, _ := row["id"].(string)
		if id == "" {
			t.Fatal("id ausente na response")
		}
		ownerID, _ := row["owner_id"].(string)
		if ownerID != user1ID {
			t.Fatalf("owner_id esperado %s, obtido %v", user1ID, ownerID)
		}
		noteID = id
	})

	t.Run("AC2_User1GetListSeesOwnNote", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		data, _ := resp["data"].([]any)
		if len(data) == 0 {
			t.Fatal("user1 deveria ver ao menos 1 nota")
		}
	})

	t.Run("AC3_User2GetListEmpty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		data, _ := resp["data"].([]any)
		if len(data) != 0 {
			t.Fatalf("user2 não deveria ver notas de user1, obtido %d item(s)", len(data))
		}
	})

	t.Run("AC4_User2GetByIDReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC5_User2PutReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodPut, basePath+"/"+noteID+"/", jsonBody(map[string]any{"title": "hack"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC6_User2DeleteReturns404", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodDelete, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer2)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("esperado 404, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC7_User1GetByIDReturns200", func(t *testing.T) {
		if noteID == "" {
			t.Skip("AC1 não gerou noteID")
		}
		req := httptest.NewRequest(http.MethodGet, basePath+"/"+noteID+"/", nil)
		req.Header.Set("Authorization", bearer1)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("esperado 200, obtido %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AC8_NoJWTOnRLSTableReturns401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("esperado 401, obtido %d", rec.Code)
		}
	})

	t.Run("AC9_TablesWithoutRLSUnaffected", func(t *testing.T) {
		app, ok := testReg.Get("testhandler")
		if !ok {
			t.Fatal("testhandler app não encontrado no registry")
		}
		tbl, ok := app.Tables[testTable]
		if !ok {
			t.Fatalf("tabela %q não encontrada", testTable)
		}
		if tbl.RLS != "" {
			t.Errorf("tabela %q não deveria ter RLS, obtido %q", testTable, tbl.RLS)
		}
	})
}
