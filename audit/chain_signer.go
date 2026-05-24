package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
)

// HMACChainSigner signs audit events using HMAC-SHA256.
type HMACChainSigner struct {
	key []byte
}

// NewHMACChainSigner creates a signer with the given key.
func NewHMACChainSigner(key []byte) *HMACChainSigner {
	return &HMACChainSigner{key: key}
}

// Sign computes HMAC-SHA256 over key event fields + prevSignature.
func (s *HMACChainSigner) Sign(event audit.AuditEvent, prevSignature string) (string, error) {
	payload := strings.Join([]string{
		event.ID,
		event.TenantID,
		event.UserID,
		event.Action,
		event.Resource,
		event.Outcome,
		event.Timestamp.Format(time.RFC3339Nano),
		event.Error,
		fmt.Sprintf("%d", event.Duration.Nanoseconds()),
		event.StepID,
		event.StepName,
		event.Status,
		event.RequestID,
		string(event.Source),
		prevSignature,
	}, "|")

	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// HMACChainVerifier verifies HMAC-SHA256 signature chains.
type HMACChainVerifier struct {
	key []byte
}

// NewHMACChainVerifier creates a verifier with the given key.
func NewHMACChainVerifier(key []byte) *HMACChainVerifier {
	return &HMACChainVerifier{key: key}
}

// VerifyChain checks the signature chain. Empty/nil chain is valid.
func (v *HMACChainVerifier) VerifyChain(events []audit.SignedEvent) audit.ChainVerificationResult {
	if len(events) == 0 {
		return audit.ChainVerificationResult{Valid: true, TotalChecked: 0, FirstBreak: -1}
	}

	signer := &HMACChainSigner{key: v.key}

	for i, se := range events {
		prevSig := ""
		if i > 0 {
			prevSig = events[i-1].Signature
		}

		expected, err := signer.Sign(se.Event, prevSig)
		if err != nil {
			return audit.ChainVerificationResult{Valid: false, TotalChecked: i, FirstBreak: i}
		}

		if !hmac.Equal([]byte(expected), []byte(se.Signature)) {
			return audit.ChainVerificationResult{Valid: false, TotalChecked: i, FirstBreak: i}
		}
	}

	return audit.ChainVerificationResult{Valid: true, TotalChecked: len(events), FirstBreak: -1}
}

// ChainIntegrityReport is a summary of chain verification results.
type ChainIntegrityReport struct {
	Valid        bool   `json:"valid"`
	TotalEvents  int    `json:"total_events"`
	Checked      int    `json:"checked"`
	FirstBreak   int    `json:"first_break"`
	Error        string `json:"error,omitempty"`
}

// VerifyFullChain queries all audit events and verifies the complete chain.
func (v *HMACChainVerifier) VerifyFullChain(events []audit.AuditEvent, signatures []string) ChainIntegrityReport {
	if len(events) != len(signatures) {
		return ChainIntegrityReport{
			Valid:       false,
			Error:       fmt.Sprintf("mismatch: %d events vs %d signatures", len(events), len(signatures)),
			TotalEvents: len(events),
		}
	}

	if len(events) == 0 {
		return ChainIntegrityReport{Valid: true, TotalEvents: 0, Checked: 0, FirstBreak: -1}
	}

	// Sort by timestamp to reconstruct chain order
	type indexed struct {
		event     audit.AuditEvent
		signature string
		index     int
	}
	items := make([]indexed, len(events))
	for i, e := range events {
		items[i] = indexed{event: e, signature: signatures[i], index: i}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].event.Timestamp.Equal(items[j].event.Timestamp) {
			return items[i].event.ID < items[j].event.ID
		}
		return items[i].event.Timestamp.Before(items[j].event.Timestamp)
	})

	signed := make([]audit.SignedEvent, len(items))
	for i, it := range items {
		signed[i] = audit.SignedEvent{Event: it.event, Signature: it.signature}
	}

	result := v.VerifyChain(signed)
	return ChainIntegrityReport{
		Valid:       result.Valid,
		TotalEvents: len(events),
		Checked:     result.TotalChecked,
		FirstBreak:  result.FirstBreak,
	}
}
