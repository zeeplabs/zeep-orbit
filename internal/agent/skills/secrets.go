package skills

import "regexp"

// credentialPatterns are well-known secret shapes. The list is a safety net
// for an operator pasting a key into a skill by mistake, not a guarantee:
// skill text is sent to the configured LLM provider, which the UI says too.
var credentialPatterns = []*regexp.Regexp{
	// PEM private keys (RSA, EC, OPENSSH, PKCS#8, ...).
	regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`),
	// OpenAI-style keys. The plain form needs a long alphanumeric run, so
	// hyphenated prose such as "sk-learn-style-guides" does not match.
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bsk-(?:proj|ant|svcacct|admin)-[A-Za-z0-9_-]{20,}`),
	// AWS access key ids.
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	// GitHub tokens.
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}`),
	// Slack tokens.
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
	// Google API keys.
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	// JWTs (header.payload.signature).
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
}

// DetectCredentials reports whether content looks like it contains a
// credential. It never returns or logs the matched text.
func DetectCredentials(content string) bool {
	for _, re := range credentialPatterns {
		if re.MatchString(content) {
			return true
		}
	}
	return false
}
