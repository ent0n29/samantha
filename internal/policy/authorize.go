package policy

import (
	"regexp"
	"strings"
)

type IntentDecision struct {
	Risk             string
	RequiresApproval bool
	Blocked          bool
	Reason           string
}

var (
	blockedIntentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\brm\s+-rf\s+/(?:\s|$)`),
		regexp.MustCompile(`(?i)\b(sudo\s+)?cat\s+.*(?:id_rsa|id_ed25519|\.env|auth\.json)`),
		regexp.MustCompile(`(?i)\b(exfiltrate|steal|dump credentials|leak secrets?)\b`),
		regexp.MustCompile(`(?i)\b(print|show|reveal)\b.*\b(api[_ -]?key|token|password|secret)\b`),
	}
	highRiskKeywords = []string{
		"delete", "remove", "drop", "truncate", "format", "wipe", "destroy",
		"shutdown", "reboot", "kill", "terminate",
		"chmod", "chown", "sudo", "install", "uninstall",
		"deploy", "push", "merge", "migrate", "write file",
	}
	mediumRiskKeywords = []string{
		"build", "create", "implement", "fix", "refactor", "update",
		"edit", "write", "add", "run", "test", "generate",
		"scaffold", "setup", "configure",
	}
)

func DecideIntent(intent string) IntentDecision {
	in := strings.ToLower(strings.TrimSpace(intent))
	if in == "" {
		return IntentDecision{
			Risk:             "low",
			RequiresApproval: false,
			Blocked:          false,
		}
	}

	for _, re := range blockedIntentPatterns {
		if re.MatchString(in) {
			return IntentDecision{
				Risk:             "blocked",
				RequiresApproval: true,
				Blocked:          true,
				Reason:           "Request appears to include destructive or secret-exfiltration behavior.",
			}
		}
	}

	for _, kw := range highRiskKeywords {
		if strings.Contains(in, kw) {
			return IntentDecision{
				Risk:             "high",
				RequiresApproval: true,
				Blocked:          false,
			}
		}
	}

	for _, kw := range mediumRiskKeywords {
		if strings.Contains(in, kw) {
			return IntentDecision{
				Risk:             "medium",
				RequiresApproval: true,
				Blocked:          false,
			}
		}
	}

	return IntentDecision{
		Risk:             "low",
		RequiresApproval: false,
		Blocked:          false,
	}
}

func LooksActionableIntent(intent string) bool {
	in := strings.ToLower(strings.TrimSpace(intent))
	if in == "" {
		return false
	}
	for _, kw := range highRiskKeywords {
		if strings.Contains(in, kw) {
			return true
		}
	}
	for _, kw := range mediumRiskKeywords {
		if strings.Contains(in, kw) {
			return true
		}
	}

	// Lightweight fallback for imperative requests with at least three words.
	parts := strings.Fields(in)
	if len(parts) < 3 {
		return false
	}
	switch parts[0] {
	case "please", "can", "could", "make", "build", "create", "write", "fix", "run":
		return true
	default:
		return false
	}
}
