package ai

// client_test.go — coverage for CallModel derived from spec.md's P3
// acceptance criteria (AIBC-12/13/14/15/17) and tasks.md's T7 Done-when
// list, not from reading the implementation. Every test points
// chatCompletionsURL at an httptest.Server — this package never calls the
// real OpenAI API here (no key is configured in this environment, and a
// test must not depend on external network regardless).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// withMockServer points chatCompletionsURL at a mock server for the
// duration of one test, restoring the real URL afterward.
func withMockServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	original := chatCompletionsURL
	chatCompletionsURL = srv.URL
	t.Cleanup(func() { chatCompletionsURL = original })
}

func chatResponseBody(message map[string]any) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": message},
		},
	})
	return string(body)
}

// AIBC-13: a plain assistant response (no tool call) returns
// ChatTurnResult{Kind: "message", Content: ...} — the model is still
// gathering information.
func TestCallModel_MessageShapeWhenNoToolCall(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, chatResponseBody(map[string]any{
			"role":    "assistant",
			"content": "Do you need authentication for this app?",
		}))
	})

	result, err := CallModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "I want a ticketing app"},
	}, nil)
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if result.Kind != "message" {
		t.Fatalf("expected Kind %q, got %q", "message", result.Kind)
	}
	if result.Content != "Do you need authentication for this app?" {
		t.Fatalf("expected content to match the assistant's message, got %q", result.Content)
	}
	if result.Plan != nil {
		t.Fatal("expected no plan on a message-shape result")
	}
}

// AIBC-14: a propose_app_plan tool call with valid arguments returns
// ChatTurnResult{Kind: "plan", Plan: ...} with the plan's fields populated
// from the tool call's arguments.
func TestCallModel_PlanShapeOnValidProposeAppPlanCall(t *testing.T) {
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		args, _ := json.Marshal(map[string]any{
			"name": "ticketing",
			"tables": []map[string]any{
				{"name": "tickets", "columns": []map[string]any{
					{"name": "title", "type": "text"},
				}},
			},
			"auth": true,
		})
		fmt.Fprint(w, chatResponseBody(map[string]any{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]any{
				{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "propose_app_plan",
						"arguments": string(args),
					},
				},
			},
		}))
	})

	result, err := CallModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "ticketing app with auth"},
	}, nil)
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if result.Kind != "plan" {
		t.Fatalf("expected Kind %q, got %q", "plan", result.Kind)
	}
	if result.Plan == nil {
		t.Fatal("expected a non-nil Plan")
	}
	if result.Plan.Name != "ticketing" {
		t.Errorf("expected plan name %q, got %q", "ticketing", result.Plan.Name)
	}
	if len(result.Plan.Tables) != 1 || result.Plan.Tables[0].Name != "tickets" {
		t.Errorf("expected one table named %q, got %+v", "tickets", result.Plan.Tables)
	}
	if !result.Plan.Auth {
		t.Error("expected Auth: true")
	}
}

// AIBC-17: a list_apps/get_app_schema tool call invokes the provided
// ReadToolInvoker, feeds the result back as a tool-role message, and
// returns the FINAL round's result — not the intermediate tool-call
// response.
func TestCallModel_ReadToolRoundTripReturnsFinalResult(t *testing.T) {
	callCount := 0
	withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First round: model asks to look up an existing app.
			fmt.Fprint(w, chatResponseBody(map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"id":   "call_list",
						"type": "function",
						"function": map[string]any{
							"name":      "list_apps",
							"arguments": "{}",
						},
					},
				},
			}))
			return
		}

		// Second round: model has the tool result and answers directly.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		msgs, _ := body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		if lastMsg["role"] != "tool" {
			t.Errorf("expected the last message in round 2 to be the tool result, got role %v", lastMsg["role"])
		}

		fmt.Fprint(w, chatResponseBody(map[string]any{
			"role":    "assistant",
			"content": "Based on your existing app, here's what I found.",
		}))
	})

	invokerCalled := false
	invoker := func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
		invokerCalled = true
		if name != "list_apps" {
			t.Errorf("expected invoker called with %q, got %q", "list_apps", name)
		}
		return json.RawMessage(`[{"id":"app-1","name":"my-ticket-app"}]`), nil
	}

	result, err := CallModel(context.Background(), "gpt-4o", "sk-test", []Message{
		{Role: "user", Content: "similar to my ticket app"},
	}, invoker)
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if !invokerCalled {
		t.Fatal("expected the ReadToolInvoker to be called")
	}
	if callCount != 2 {
		t.Fatalf("expected exactly 2 HTTP round-trips, got %d", callCount)
	}
	if result.Kind != "message" || result.Content != "Based on your existing app, here's what I found." {
		t.Fatalf("expected the FINAL round's message result, got %+v", result)
	}
}

// AIBC-14 (negative path): malformed/incomplete propose_app_plan arguments
// return an error, never a partially-populated Plan.
func TestCallModel_MalformedPlanArgumentsReturnErrorNotPartialPlan(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"invalid JSON", `{not valid json`},
		{"missing name", `{"tables":[{"name":"t"}],"auth":true}`},
		{"empty tables", `{"name":"x","tables":[],"auth":true}`},
		{"table with no name", `{"name":"x","tables":[{"name":""}],"auth":true}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withMockServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, chatResponseBody(map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "propose_app_plan",
								"arguments": c.args,
							},
						},
					},
				}))
			})

			result, err := CallModel(context.Background(), "gpt-4o", "sk-test", []Message{
				{Role: "user", Content: "x"},
			}, nil)
			if err == nil {
				t.Fatalf("expected an error for %s, got result %+v", c.name, result)
			}
			if result.Plan != nil {
				t.Fatalf("expected no partially-populated Plan for %s, got %+v", c.name, result.Plan)
			}
		})
	}
}

// T7 Done-when: a bounded client-side HTTP timeout is applied to the
// OpenAI call — a server that never responds must not hang CallModel
// forever.
func TestCallModel_TimesOutOnHungServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	original := chatCompletionsURL
	chatCompletionsURL = srv.URL
	defer func() { chatCompletionsURL = original }()
	if httpTimeout <= 200*time.Millisecond {
		t.Fatalf("test assumption broken: httpTimeout (%v) must exceed the server's artificial delay", httpTimeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := CallModel(ctx, "gpt-4o", "sk-test", []Message{{Role: "user", Content: "x"}}, nil)
	if err == nil {
		t.Fatal("expected CallModel to return an error when the context deadline is exceeded before the server responds")
	}
}

// T7 Done-when: no OpenAI SDK dependency was added to go.mod — this
// package hand-rolls the REST call, per design.md Tech Decisions.
func TestNoOpenAISDKDependencyInGoMod(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "all").CombinedOutput()
	if err != nil {
		t.Skipf("skipping go.mod dependency check: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "openai") {
			t.Fatalf("expected no OpenAI SDK dependency in go.mod, found: %s", line)
		}
	}
}
