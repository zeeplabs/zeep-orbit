//go:build providercontract

// Real-endpoint provider contract run. It reuses the exact case table CI runs
// against the httptest fake (contractCases/runContract in contract_test.go);
// this file only wires providers from the environment. It is excluded from
// normal builds by the providercontract tag because it spends real tokens.
//
// Env:
//
//	CONTRACT_PROVIDERS  comma-separated entries, each
//	                    <name>:<model>[@<base_url>][#<KEYENV>]
//	                    name      openai | openai_compatible (up to the FIRST ':')
//	                    model     between that ':' and the optional '@' (may contain ':')
//	                    base_url  after the LAST '@'; required for openai_compatible
//	                    KEYENV    after the LAST '#': name of the env var holding the
//	                              API key; default CONTRACT_KEY_<NAME uppercased>
//	CONTRACT_KEY_OPENAI, CONTRACT_KEY_OPENAI_COMPATIBLE, ...  API keys. An entry whose
//	                    key env var is unset is skipped; set it to the empty string
//	                    for local servers without auth. Keys are never printed.
//
// Empty CONTRACT_PROVIDERS skips the test. Example (the model ids are
// illustrative; use ids your accounts actually have):
//
//	CONTRACT_PROVIDERS='openai:gpt-4.1-mini,openai_compatible:gemini-2.5-flash@https://generativelanguage.googleapis.com/v1beta/openai/#CONTRACT_KEY_GEMINI' \
//	CONTRACT_KEY_OPENAI=... CONTRACT_KEY_GEMINI=... \
//	go test -tags providercontract -run Contract -v ./internal/agent/provider/
//
// Output: one machine-readable line per provider/case, ready to paste into
// design.md Risks:
//
//	CONTRACT_RESULT provider=<name> model=<id> case=<case> result=PASS|FAIL|SKIP note="..."
//
// The parallel_tool_calls case reports "parallel_supported=yes|no" in note; it
// fails only on malformed calls, because a model may legitimately return one.
package provider

import (
	"os"
	"strings"
	"testing"
)

func TestContractReal(t *testing.T) {
	spec := os.Getenv("CONTRACT_PROVIDERS")
	if strings.TrimSpace(spec) == "" {
		t.Skip("CONTRACT_PROVIDERS is empty; nothing to run against real endpoints")
	}
	entries, err := parseContractEntries(spec)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, e := range entries {
		t.Run(e.Name+"/"+e.Model, func(t *testing.T) {
			p, skip, err := buildContractProvider(e, os.LookupEnv)
			if err != nil {
				t.Fatalf("build provider: %v", err)
			}
			if p == nil {
				t.Logf("CONTRACT_RESULT provider=%s model=%s case=* result=SKIP note=%q", e.Name, e.Model, skip)
				t.Skip(skip)
			}
			runContract(t, p)
		})
	}
}
