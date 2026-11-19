package agent

import "github.com/zeeplabs/zeep-orbit/internal/agent/provider"

// estimateCharsPerToken is the chars-per-token ratio of the usage estimate,
// the same heuristic as the compaction threshold. It counts bytes, not runes,
// so non-ASCII text (Portuguese, code with symbols) errs on the high side.
const estimateCharsPerToken = 4

// recordedUsage is the usage the ledger gets for one call. A provider that
// reported usage is recorded as it reported it. One that did not (an
// openai_compatible server may omit the usage object) would otherwise be
// recorded as zero tokens, and the token and cost caps would never fire on
// it. So the call is recorded with a conservative estimate instead, still
// with Reported=false, which keeps it apart in the ledger (usage_reported)
// and out of the build estimate:
//
//   - input: every byte that was sent (system prompt, tool specs, messages and
//     their tool calls) / 4, rounded up;
//   - output: the reply's own size / 4, or the call's max_tokens when it is
//     larger, because a reasoning model also bills thinking it does not
//     return and max_tokens is the most the provider could have generated.
//
// A field the provider did send is never lowered by the estimate.
func recordedUsage(req provider.ChatRequest, resp provider.ChatResponse) provider.Usage {
	if resp.Usage.Reported {
		return resp.Usage
	}
	in := len(req.System)
	for _, t := range req.Tools {
		in += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	for _, m := range req.Messages {
		in += len(m.Content) + len(m.ToolCallID) + callsSize(m.ToolCalls)
	}
	calls := resp.Message.ToolCalls
	if len(calls) == 0 {
		calls = resp.ToolCalls
	}
	out := len(resp.Message.Content) + callsSize(calls)

	u := resp.Usage
	u.InputTokens = max(u.InputTokens, ceilDiv(in, estimateCharsPerToken))
	u.OutputTokens = max(u.OutputTokens, ceilDiv(out, estimateCharsPerToken), req.MaxOutputTokens)
	return u
}

func callsSize(calls []provider.ToolCall) int {
	n := 0
	for _, c := range calls {
		n += len(c.ID) + len(c.Name) + len(c.Arguments)
	}
	return n
}

func ceilDiv(n, d int) int {
	return (n + d - 1) / d
}
