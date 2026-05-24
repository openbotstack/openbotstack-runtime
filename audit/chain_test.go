package audit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
)

// === 正常路径 (1) ===

func TestHMACChainSigner_SignAndVerify(t *testing.T) {
	key := []byte("test-secret-key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt1 := audit.AuditEvent{ID: "evt-1", Action: "skills.execute", TenantID: "t1", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig1, err := signer.Sign(evt1, "")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	evt2 := audit.AuditEvent{ID: "evt-2", Action: "skills.execute", TenantID: "t1", Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)}
	sig2, err := signer.Sign(evt2, sig1)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	chain := []audit.SignedEvent{
		{Event: evt1, Signature: sig1},
		{Event: evt2, Signature: sig2},
	}
	result := verifier.VerifyChain(chain)
	if !result.Valid {
		t.Errorf("valid chain should pass, FirstBreak=%d", result.FirstBreak)
	}
	if result.TotalChecked != 2 {
		t.Errorf("TotalChecked=%d, want 2", result.TotalChecked)
	}
}

// === 异常路径 (12) ===

func TestHMACChainSigner_EmptyEvent(t *testing.T) {
	signer := NewHMACChainSigner([]byte("key"))
	sig, err := signer.Sign(audit.AuditEvent{}, "")
	if err != nil {
		t.Fatalf("empty event should not error: %v", err)
	}
	if sig == "" {
		t.Error("should produce signature even for empty event")
	}
}

func TestHMACChainSigner_EmptyKey(t *testing.T) {
	signer := NewHMACChainSigner([]byte{})
	sig, err := signer.Sign(audit.AuditEvent{ID: "evt-1"}, "")
	if err != nil {
		t.Fatalf("empty key should not error: %v", err)
	}
	if sig == "" {
		t.Error("empty key should still produce signature")
	}
}

func TestHMACChainSigner_Deterministic(t *testing.T) {
	signer := NewHMACChainSigner([]byte("key"))
	evt := audit.AuditEvent{ID: "evt-1", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig1, _ := signer.Sign(evt, "")
	sig2, _ := signer.Sign(evt, "")
	if sig1 != sig2 {
		t.Error("same inputs should produce same signature")
	}
}

func TestHMACChainSigner_DifferentKeys(t *testing.T) {
	evt := audit.AuditEvent{ID: "evt-1"}
	s1 := NewHMACChainSigner([]byte("key-a"))
	s2 := NewHMACChainSigner([]byte("key-b"))
	sig1, _ := s1.Sign(evt, "")
	sig2, _ := s2.Sign(evt, "")
	if sig1 == sig2 {
		t.Error("different keys should produce different signatures")
	}
}

func TestHMACChainSigner_LongKey(t *testing.T) {
	longKey := make([]byte, 1024)
	for i := range longKey {
		longKey[i] = byte(i % 256)
	}
	signer := NewHMACChainSigner(longKey)
	sig, err := signer.Sign(audit.AuditEvent{ID: "evt-1"}, "")
	if err != nil {
		t.Fatalf("long key should not error: %v", err)
	}
	if sig == "" {
		t.Error("long key should produce signature")
	}
}

func TestHMACChainSigner_DifferentPrevSignature(t *testing.T) {
	signer := NewHMACChainSigner([]byte("key"))
	evt := audit.AuditEvent{ID: "evt-2", Action: "test"}
	sig1, _ := signer.Sign(evt, "prev-a")
	sig2, _ := signer.Sign(evt, "prev-b")
	if sig1 == sig2 {
		t.Error("different prevSignature should produce different signature")
	}
}

func TestHMACChainVerifier_EmptyChain(t *testing.T) {
	verifier := NewHMACChainVerifier([]byte("key"))
	result := verifier.VerifyChain(nil)
	if !result.Valid {
		t.Error("nil chain should be valid")
	}

	result = verifier.VerifyChain([]audit.SignedEvent{})
	if !result.Valid {
		t.Error("empty chain should be valid")
	}
}

func TestHMACChainVerifier_TamperedEvent(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt1 := audit.AuditEvent{ID: "evt-1", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	evt2 := audit.AuditEvent{ID: "evt-2", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)}
	sig1, _ := signer.Sign(evt1, "")
	sig2, _ := signer.Sign(evt2, sig1)

	chain := []audit.SignedEvent{
		{Event: evt1, Signature: sig1},
		{Event: audit.AuditEvent{ID: "evt-2", Action: "TAMPERED", Timestamp: evt2.Timestamp}, Signature: sig2},
	}

	result := verifier.VerifyChain(chain)
	if result.Valid {
		t.Error("tampered event should be detected")
	}
	if result.FirstBreak != 1 {
		t.Errorf("FirstBreak=%d, want 1", result.FirstBreak)
	}
}

func TestHMACChainVerifier_TamperedSignature(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt1 := audit.AuditEvent{ID: "evt-1", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	_, _ = signer.Sign(evt1, "")

	chain := []audit.SignedEvent{
		{Event: evt1, Signature: "tampered-sig"},
	}

	result := verifier.VerifyChain(chain)
	if result.Valid {
		t.Error("tampered signature should fail")
	}
}

func TestHMACChainVerifier_WrongKey(t *testing.T) {
	signer := NewHMACChainSigner([]byte("key-a"))
	verifier := NewHMACChainVerifier([]byte("key-b"))

	evt := audit.AuditEvent{ID: "evt-1", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig, _ := signer.Sign(evt, "")

	chain := []audit.SignedEvent{{Event: evt, Signature: sig}}
	result := verifier.VerifyChain(chain)
	if result.Valid {
		t.Error("wrong key should fail verification")
	}
}

func TestHMACChainVerifier_SingleEventValid(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt := audit.AuditEvent{ID: "evt-1", Action: "test", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig, _ := signer.Sign(evt, "")

	result := verifier.VerifyChain([]audit.SignedEvent{{Event: evt, Signature: sig}})
	if !result.Valid {
		t.Error("single valid event should pass")
	}
	if result.TotalChecked != 1 {
		t.Errorf("TotalChecked=%d, want 1", result.TotalChecked)
	}
}

func TestHMACChainVerifier_TamperedTenantID(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt := audit.AuditEvent{ID: "evt-1", TenantID: "tenant-a", Action: "test", Outcome: "success", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig, _ := signer.Sign(evt, "")

	tampered := audit.AuditEvent{ID: "evt-1", TenantID: "tenant-b", Action: "test", Outcome: "success", Timestamp: evt.Timestamp}
	chain := []audit.SignedEvent{{Event: tampered, Signature: sig}}

	result := verifier.VerifyChain(chain)
	if result.Valid {
		t.Error("tampered TenantID should be detected")
	}
}

func TestHMACChainVerifier_TamperedOutcome(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt := audit.AuditEvent{ID: "evt-1", Action: "test", Outcome: "success", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sig, _ := signer.Sign(evt, "")

	tampered := audit.AuditEvent{ID: "evt-1", Action: "test", Outcome: "failure", Timestamp: evt.Timestamp}
	chain := []audit.SignedEvent{{Event: tampered, Signature: sig}}

	result := verifier.VerifyChain(chain)
	if result.Valid {
		t.Error("tampered Outcome should be detected")
	}
}

func TestVerifyFullChain_MismatchedLengths(t *testing.T) {
	verifier := NewHMACChainVerifier([]byte("key"))
	report := verifier.VerifyFullChain(
		[]audit.AuditEvent{{ID: "a"}, {ID: "b"}},
		[]string{"sig-a"},
	)
	if report.Valid {
		t.Error("mismatched lengths should fail")
	}
	if report.Error == "" {
		t.Error("should have error message")
	}
}

func TestVerifyFullChain_EmptyInput(t *testing.T) {
	verifier := NewHMACChainVerifier([]byte("key"))
	report := verifier.VerifyFullChain(nil, nil)
	if !report.Valid {
		t.Error("empty input should be valid")
	}
}

func TestVerifyFullChain_OutOfOrder(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	evt1 := audit.AuditEvent{ID: "a", Action: "first", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	evt2 := audit.AuditEvent{ID: "b", Action: "second", Timestamp: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)}
	sig1, _ := signer.Sign(evt1, "")
	sig2, _ := signer.Sign(evt2, sig1)

	// Pass in reverse order — VerifyFullChain should sort and verify correctly
	report := verifier.VerifyFullChain(
		[]audit.AuditEvent{evt2, evt1},
		[]string{sig2, sig1},
	)
	if !report.Valid {
		t.Errorf("out-of-order events should sort and validate, FirstBreak=%d", report.FirstBreak)
	}
}

func TestVerifyFullChain_EqualTimestamps(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	evt1 := audit.AuditEvent{ID: "a", Action: "test", Timestamp: ts}
	evt2 := audit.AuditEvent{ID: "b", Action: "test", Timestamp: ts}
	sig1, _ := signer.Sign(evt1, "")
	sig2, _ := signer.Sign(evt2, sig1)

	report := verifier.VerifyFullChain(
		[]audit.AuditEvent{evt1, evt2},
		[]string{sig1, sig2},
	)
	if !report.Valid {
		t.Errorf("equal timestamps with different IDs should sort stably, FirstBreak=%d", report.FirstBreak)
	}
}

func TestHMACChainVerifier_ConcurrentVerification(t *testing.T) {
	key := []byte("key")
	signer := NewHMACChainSigner(key)
	verifier := NewHMACChainVerifier(key)

	// Build a unique chain
	chain := make([]audit.SignedEvent, 50)
	for i := range chain {
		prevSig := ""
		if i > 0 {
			prevSig = chain[i-1].Signature
		}
		evt := audit.AuditEvent{ID: fmt.Sprintf("evt-%d", i), Action: "test"}
		sig, _ := signer.Sign(evt, prevSig)
		chain[i] = audit.SignedEvent{Event: evt, Signature: sig}
	}

	var wg sync.WaitGroup
	errCh := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := verifier.VerifyChain(chain)
			if !result.Valid {
				errCh <- fmt.Sprintf("concurrent verification failed: FirstBreak=%d", result.FirstBreak)
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for msg := range errCh {
		t.Error(msg)
	}
}
