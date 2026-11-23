package planning

import (
	"context"
	"encoding/json"

	"github.com/zeeplabs/zeep-orbit/internal/agent/tools"
)

const askUserDescription = "Ask the user a round of questions before building, with suggested answers they can click. " +
	"The turn pauses until the user answers. Use it for what you need to know and cannot read from the user's messages, the app brief or the backend schema: " +
	"what the app is for and who uses it, what it stores, who signs in and what each kind of user may do, the main screens and flows, the look and the interface language, and what to leave out. " +
	"Group everything you need in one round (at most 4 questions). Write in the user's language and in their terms, never about implementation (frameworks, tables, column types). " +
	"Every option is a complete, concrete answer. Do not add \"other\" or \"you decide\" options: the user can always type their own answer or let you decide. " +
	"When you ask about the look, offer directions that fit this app's audience and job (each a mood plus a palette idea in the user's words), never a generic pair such as \"clean and light\" against \"dark\"."

const askUserSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["questions"],
  "properties": {
    "questions": {
      "type": "array", "minItems": 1, "maxItems": 4,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "title", "question", "options"],
        "properties": {
          "id": {"type": "string", "description": "Short lowercase identifier, unique in the round, for example \"sign_in\"."},
          "title": {"type": "string", "description": "Two or three words shown above the question (at most 40 characters)."},
          "question": {"type": "string", "description": "The question, one or two sentences (at most 300 characters)."},
          "options": {
            "type": "array", "minItems": 2, "maxItems": 5,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["label"],
              "properties": {
                "label": {"type": "string", "description": "The answer as the user would say it (at most 80 characters)."},
                "description": {"type": "string", "description": "Optional: what choosing it means (at most 160 characters)."}
              }
            }
          },
          "multiple": {"type": "boolean", "description": "true when several options can be chosen together."}
        }
      }
    }
  }
}`

const proposeBuildPlanDescription = "Propose the build plan for the user to approve. It does NOT build anything: the turn pauses until the user approves it, asks for changes or goes back to questions. " +
	"Once approved it becomes the app brief you receive on every later turn. " +
	"\"title\" is the project's short name; choose it from what the app is, and ask the user for a name only when the project is ambiguous or they want to choose one. " +
	"Put everything you decided without the user saying it under \"assumptions\". " +
	"\"data\" is what the app stores, in words, and who can reach it; the tables themselves are created later with a backend proposal. " +
	"\"milestones\" are in build order and each one is small enough to end in one working build the user can see; the first one must already show something usable. " +
	"\"look\" needs a \"direction\" chosen for this app: who uses it, where and for what, and a mood that fits, not the default of a plain light page with one accent color; fill \"type\", \"density\" and \"motion\" to match, and keep what the user asked for in \"style\" and \"colors\". " +
	"Keep it short: one line per item, written in the user's language. When you revise an approved plan, send the whole plan again and mark milestones that are already built with \"done\": true."

const proposeBuildPlanSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "summary", "screens", "look", "assumptions", "milestones"],
  "properties": {
    "title": {"type": "string", "description": "The project's short name, 2 to 5 words, in the user's language, for example \"Agenda da Clínica\" (at most 60 characters). Not a sentence."},
    "summary": {"type": "string", "description": "One paragraph: what the app is, for whom (at most 600 characters)."},
    "screens": {
      "type": "array", "minItems": 1, "maxItems": 20,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["name", "purpose"],
        "properties": {
          "name": {"type": "string"},
          "purpose": {"type": "string", "description": "What the user does there, one line."}
        }
      }
    },
    "data": {
      "type": "array", "maxItems": 20,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["table", "holds", "access"],
        "properties": {
          "table": {"type": "string", "description": "Name of the thing stored, for example \"patients\"."},
          "holds": {"type": "string", "description": "What it holds, one line."},
          "access": {"type": "string", "enum": ["public", "signed_in", "owner", "role"], "description": "Who can reach it: public (anyone, signed in or not), signed_in (any signed-in user; sign-up is open, so anyone can create an account and this is NOT limited to a team), owner (each user only their own rows), role (depends on the user's role; explain in access_note)."},
          "access_note": {"type": "string", "description": "Required for role: which role can do what."}
        }
      }
    },
    "look": {
      "type": "object", "additionalProperties": false, "required": ["direction"],
      "properties": {
        "direction": {"type": "string", "description": "The visual direction in a few words, chosen for this app's audience and job, for example \"calm and editorial\" or \"dense and operational\" (at most 100 characters)."},
        "type": {"type": "string", "description": "Typographic feel, using system fonts (at most 160 characters)."},
        "density": {"type": "string", "description": "airy, balanced or compact."},
        "motion": {"type": "string", "description": "Where movement goes and why: page entry, state changes, feedback to an action; or none (at most 200 characters)."},
        "style": {"type": "string", "description": "What the user asked for, in their words."},
        "colors": {"type": "string", "description": "What the user asked for, in their words, or the palette idea of the direction."},
        "ui_language": {"type": "string", "description": "Language of the app's interface, for example \"pt-BR\"."},
        "reference": {"type": "string", "description": "A site or product the user wants it to resemble, if any."}
      }
    },
    "assumptions": {"type": "array", "maxItems": 15, "items": {"type": "string"}, "description": "What you decided without the user saying it. Empty list when there is nothing."},
    "out_of_scope": {"type": "array", "maxItems": 10, "items": {"type": "string"}},
    "milestones": {
      "type": "array", "minItems": 1, "maxItems": 8,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["title", "deliverable"],
        "properties": {
          "title": {"type": "string"},
          "deliverable": {"type": "string", "description": "What the user can see or do when it is built, one line."},
          "done": {"type": "boolean", "description": "Only when revising an approved plan: true for a milestone that is already built."}
        }
      }
    }
  }
}`

// pausedResult is the content of a planning call that validated. The loop
// pauses the turn on the tool's kind, so the model never reads it: the real
// result is written when the user decides.
const pausedResult = "Waiting for the user."

// Tools returns the two planning tools. Their handlers only validate the
// arguments (a bad call is an error result the model corrects); the agent
// loop pauses the turn on [tools.KindPlanning].
func Tools() []tools.Tool {
	return []tools.Tool{
		{
			Name:        ToolAskUser,
			Description: askUserDescription,
			Schema:      json.RawMessage(askUserSchema),
			Kind:        tools.KindPlanning,
			Handler: func(_ context.Context, call tools.Call) (tools.Result, error) {
				if _, err := ParseQuestions(call.Arguments); err != nil {
					return tools.Result{}, tools.ToolErrorf("%s", err.Error())
				}
				return tools.Result{Content: pausedResult}, nil
			},
		},
		{
			Name:        ToolProposeBuildPlan,
			Description: proposeBuildPlanDescription,
			Schema:      json.RawMessage(proposeBuildPlanSchema),
			Kind:        tools.KindPlanning,
			Handler: func(_ context.Context, call tools.Call) (tools.Result, error) {
				plan, err := ParsePlan(call.Arguments)
				if err == nil {
					err = plan.RequireTitle()
				}
				if err == nil {
					err = plan.RequireLook()
				}
				if err != nil {
					return tools.Result{}, tools.ToolErrorf("%s", err.Error())
				}
				return tools.Result{Content: pausedResult}, nil
			},
		},
	}
}
