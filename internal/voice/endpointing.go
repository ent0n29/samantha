package voice

import (
	"regexp"
	"strings"
	"time"
)

type semanticEndpointHint struct {
	Reason       string
	Confidence   float64
	Hold         time.Duration
	ShouldCommit bool
}

type semanticEndpointDispatchState struct {
	hasValue       bool
	lastReason     string
	lastHoldBucket int
	lastCommitFlag bool
	lastConfBucket int
	lastSentAt     time.Time
}

const (
	semanticHoldMin              = 40 * time.Millisecond
	semanticHoldMax              = 900 * time.Millisecond
	semanticEmitRefresh          = 1200 * time.Millisecond
	semanticEmitHoldBucketWidth  = 80
	semanticEmitConfBucketWidth  = 10
	semanticConfidenceUnknown    = 0.55
	semanticConfidenceCommitSafe = 0.50
)

var (
	semanticContinuationTailRe = regexp.MustCompile(`(?i)\b(and|but|because|so|then|which|that|if|when|while|as|to|for)\s*$`)
	semanticContinuationHeadRe = regexp.MustCompile(`(?i)^(and|but|because|so|then)\b`)
	semanticContinuationPhrase = regexp.MustCompile(`(?i)\b(i mean|for example|for instance|in order to)\s*$`)
	semanticTerminalTailRe     = regexp.MustCompile(`(?i)([.!?]["']?\s*$|\b(done|thanks|thank you|that's all|thats all)\s*$)`)
	semanticOpenTailRe         = regexp.MustCompile(`[,;:\-…]\s*$`)
)

func buildSemanticEndpointHint(partial string, confidence float64, utteranceAge time.Duration) (semanticEndpointHint, bool) {
	normalized := normalizeSemanticEndpointText(partial)
	if normalized == "" {
		return semanticEndpointHint{}, false
	}

	confidence = normalizeSemanticConfidence(confidence)
	hint := semanticEndpointHint{
		Reason:     "neutral",
		Confidence: maxFloat(0.58, confidence),
		Hold:       210 * time.Millisecond,
	}

	continuation := hasSemanticContinuationCue(normalized)
	terminal := hasSemanticTerminalCue(normalized)
	if continuation {
		hint.Reason = "continuation"
		hint.Confidence = maxFloat(hint.Confidence, 0.86)
		hint.Hold = 520 * time.Millisecond
	}
	if terminal {
		hint.Reason = "terminal"
		hint.Confidence = maxFloat(hint.Confidence, 0.82)
		hint.Hold = 90 * time.Millisecond
		hint.ShouldCommit = confidence >= semanticConfidenceCommitSafe
	}

	if utteranceAge > 6*time.Second && !continuation {
		hint.Reason = "long_utterance"
		hint.Hold -= 70 * time.Millisecond
	}

	if utteranceAge > 0 && utteranceAge < 700*time.Millisecond {
		hint.Hold += 110 * time.Millisecond
		if hint.Reason == "neutral" {
			hint.Reason = "short_utterance"
		}
	}

	if confidence < 0.45 {
		hint.Hold += 140 * time.Millisecond
		hint.Confidence = minFloat(hint.Confidence, 0.62)
		hint.ShouldCommit = false
		if hint.Reason == "neutral" || hint.Reason == "terminal" {
			hint.Reason = "low_confidence"
		}
	}

	hint.Hold = clampDuration(hint.Hold, semanticHoldMin, semanticHoldMax)
	hint.Confidence = clampFloat(hint.Confidence, 0.05, 0.99)
	return hint, true
}

func (s *semanticEndpointDispatchState) ShouldEmit(h semanticEndpointHint, now time.Time) bool {
	reason := strings.TrimSpace(strings.ToLower(h.Reason))
	if reason == "" {
		reason = "neutral"
	}
	holdBucket := holdBucketForHint(h.Hold)
	confBucket := confidenceBucketForHint(h.Confidence)
	if !s.hasValue {
		s.set(reason, holdBucket, h.ShouldCommit, confBucket, now)
		return true
	}
	if reason != s.lastReason ||
		holdBucket != s.lastHoldBucket ||
		h.ShouldCommit != s.lastCommitFlag ||
		confBucket != s.lastConfBucket ||
		now.Sub(s.lastSentAt) >= semanticEmitRefresh {
		s.set(reason, holdBucket, h.ShouldCommit, confBucket, now)
		return true
	}
	return false
}

func (s *semanticEndpointDispatchState) set(reason string, holdBucket int, shouldCommit bool, confBucket int, now time.Time) {
	s.hasValue = true
	s.lastReason = reason
	s.lastHoldBucket = holdBucket
	s.lastCommitFlag = shouldCommit
	s.lastConfBucket = confBucket
	s.lastSentAt = now
}

func (s *semanticEndpointDispatchState) Reset() {
	s.hasValue = false
	s.lastReason = ""
	s.lastHoldBucket = 0
	s.lastCommitFlag = false
	s.lastConfBucket = 0
	s.lastSentAt = time.Time{}
}

func normalizeSemanticEndpointText(raw string) string {
	return strings.TrimSpace(strings.ToLower(raw))
}

func hasSemanticContinuationCue(normalized string) bool {
	if normalized == "" {
		return false
	}
	if semanticOpenTailRe.MatchString(normalized) {
		return true
	}
	if semanticContinuationHeadRe.MatchString(normalized) {
		return true
	}
	if semanticContinuationTailRe.MatchString(normalized) {
		return true
	}
	if semanticContinuationPhrase.MatchString(normalized) {
		return true
	}
	return false
}

func hasSemanticTerminalCue(normalized string) bool {
	if normalized == "" {
		return false
	}
	if semanticOpenTailRe.MatchString(normalized) {
		return false
	}
	return semanticTerminalTailRe.MatchString(normalized)
}

func normalizeSemanticConfidence(conf float64) float64 {
	if conf <= 0 || conf > 1 {
		return semanticConfidenceUnknown
	}
	return conf
}

func holdBucketForHint(d time.Duration) int {
	ms := int(d.Milliseconds())
	if ms <= 0 {
		return 0
	}
	return ms / semanticEmitHoldBucketWidth
}

func confidenceBucketForHint(c float64) int {
	v := int(clampFloat(c, 0, 1) * 100)
	return v / semanticEmitConfBucketWidth
}

func clampDuration(v, min, max time.Duration) time.Duration {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
