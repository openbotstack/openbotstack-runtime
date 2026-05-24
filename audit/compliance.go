package audit

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// ComplianceReportRequest specifies parameters for generating a compliance report.
type ComplianceReportRequest struct {
	// Scope defines the reporting scope.
	Scope audit.ComplianceScope

	// Period is the time range to cover.
	Period audit.TimeRange

	// Signatures optionally provides chain signatures for integrity verification.
	Signatures []string

	// RetentionNow overrides "now" for retention compliance checks (for tests).
	// Defaults to time.Now() if zero.
	RetentionNow time.Time
}

// ComplianceReportGenerator aggregates audit events into compliance reports.
type ComplianceReportGenerator struct {
	querier   ComplianceEventQuerier
	signingKey []byte
	retention *RetentionPolicy
}

// ComplianceEventQuerier queries audit events for report generation.
type ComplianceEventQuerier interface {
	Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error)
}

// SignatureQuerier retrieves chain signatures alongside audit events.
type SignatureQuerier interface {
	QueryWithSignatures(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, []string, error)
}

// NewComplianceReportGenerator creates a report generator with the given querier
// and optional HMAC signing key for chain integrity verification.
func NewComplianceReportGenerator(querier ComplianceEventQuerier, signingKey []byte) *ComplianceReportGenerator {
	return &ComplianceReportGenerator{
		querier:    querier,
		signingKey: signingKey,
	}
}

// SetRetentionPolicy configures retention compliance checking.
func (g *ComplianceReportGenerator) SetRetentionPolicy(policy *RetentionPolicy) {
	g.retention = policy
}

// Generate produces a compliance report for the given request scope and period.
func (g *ComplianceReportGenerator) Generate(ctx context.Context, req ComplianceReportRequest) (*audit.ComplianceReport, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compliance: context is required")
	}

	// Query audit events for the scope and period
	filter := execution_logs.QueryFilter{
		TenantID: req.Scope.TenantID,
		UserID:   req.Scope.UserID,
		From:     req.Period.From,
		To:       req.Period.To,
		Limit:    50000, // bounded upper limit
	}

	events, err := g.querier.Query(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("compliance: query failed: %w", err)
	}

	report := &audit.ComplianceReport{
		ID:          uuid.NewString(),
		GeneratedAt: time.Now(),
		Scope:       req.Scope,
		Period:      req.Period,
	}

	// Build each section
	report.Summary = buildSummary(events)
	report.PolicyCompliance = buildPolicyCompliance(events)
	report.ExecutionHealth = buildExecutionHealth(events)

	// Use pre-provided signatures, or query from storage if available.
	// When querying signatures, we get events in ASC order (for chain reconstruction),
	// so we use those events for chain verification to ensure correct pairing.
	signatures := req.Signatures
	chainEvents := events
	if len(signatures) == 0 {
		if sq, ok := g.querier.(SignatureQuerier); ok {
			ascEvents, sigs, err := sq.QueryWithSignatures(ctx, filter)
			if err == nil {
				signatures = sigs
				chainEvents = ascEvents
			}
		}
	}
	report.ChainIntegrity = g.buildChainIntegrity(chainEvents, signatures)
	report.RetentionCompliance = g.buildRetentionCompliance(events, req)
	report.TopErrors = buildTopErrors(events, 10)

	return report, nil
}

// buildSummary computes high-level statistics from events.
func buildSummary(events []audit.AuditEvent) audit.ComplianceSummary {
	s := audit.ComplianceSummary{
		TotalEvents: len(events),
	}

	executionIDs := make(map[string]struct{})
	var errorCount int
	var policyCount, deniedCount int

	for _, e := range events {
		if e.RequestID != "" {
			executionIDs[e.RequestID] = struct{}{}
		}
		if e.Outcome == "failure" || e.Error != "" {
			errorCount++
		}
		if e.Action == "policy.check" || e.Action == "policy.enforce" {
			policyCount++
			if e.Outcome == "denied" {
				deniedCount++
			}
		}
	}

	s.TotalExecutions = len(executionIDs)

	if len(events) > 0 {
		s.ErrorRate = float64(errorCount) / float64(len(events)) * 100
	}
	if policyCount > 0 {
		s.DenialRate = float64(deniedCount) / float64(policyCount) * 100
	}

	return s
}

// buildPolicyCompliance summarizes policy enforcement outcomes.
func buildPolicyCompliance(events []audit.AuditEvent) audit.PolicyComplianceSection {
	var pc audit.PolicyComplianceSection
	deniedActions := make(map[string]int)

	for _, e := range events {
		if e.Action != "policy.check" && e.Action != "policy.enforce" {
			continue
		}
		pc.TotalChecks++
		if e.Outcome == "allowed" {
			pc.Allowed++
		} else if e.Outcome == "denied" {
			pc.Denied++
			if e.Resource != "" {
				deniedActions[e.Resource]++
			}
		}
	}

	if pc.TotalChecks > 0 {
		pc.DenialRate = float64(pc.Denied) / float64(pc.TotalChecks) * 100
	}

	// Build top denied actions (sorted by count descending, max 10)
	pc.TopDeniedActions = topN(deniedActions, 10)

	return pc
}

// buildExecutionHealth summarizes execution step outcomes and durations.
func buildExecutionHealth(events []audit.AuditEvent) audit.ExecutionHealthSection {
	var eh audit.ExecutionHealthSection
	var totalDuration time.Duration
	var durations []int64
	failureBySource := make(map[string]int)

	for _, e := range events {
		if e.StepID == "" {
			continue
		}
		eh.StepsTotal++

		if e.Status == "completed" {
			eh.StepsCompleted++
		} else if e.Status == "failed" {
			eh.StepsFailed++
			src := string(e.Source)
			if src == "" {
				src = "unknown"
			}
			failureBySource[src]++
		}

		if e.Duration > 0 {
			totalDuration += e.Duration
			durations = append(durations, e.Duration.Milliseconds())
		}
	}

	if eh.StepsTotal > 0 {
		eh.SuccessRate = float64(eh.StepsCompleted) / float64(eh.StepsTotal) * 100
	}

	if len(durations) > 0 {
		eh.AvgDurationMs = totalDuration.Milliseconds() / int64(len(durations))
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		eh.P99DurationMs = durations[p99Index(len(durations))]
	}

	if len(failureBySource) > 0 {
		eh.FailureBreakdown = failureBySource
	}

	return eh
}

// p99Index returns the index for the 99th percentile value.
func p99Index(n int) int {
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(n)*0.99)) - 1
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// buildChainIntegrity verifies the HMAC chain for the given events.
func (g *ComplianceReportGenerator) buildChainIntegrity(events []audit.AuditEvent, signatures []string) audit.ChainIntegritySection {
	ci := audit.ChainIntegritySection{
		TotalEvents: len(events),
	}

	if len(events) == 0 || len(signatures) == 0 {
		ci.Verified = true
		return ci
	}

	if g.signingKey == nil {
		// No key configured -- cannot verify, report as not verified
		ci.Verified = false
		return ci
	}

	verifier := NewHMACChainVerifier(g.signingKey)
	report := verifier.VerifyFullChain(events, signatures)

	ci.Verified = report.Valid
	ci.FirstBreakIndex = report.FirstBreak

	// Count breaks
	if !report.Valid {
		// Re-verify to count all breaks
		ci.BreakCount = countChainBreaks(events, signatures, g.signingKey)
	}

	return ci
}

// countChainBreaks counts the number of chain breaks by verifying each link.
func countChainBreaks(events []audit.AuditEvent, signatures []string, key []byte) int {
	if len(events) != len(signatures) || len(events) == 0 {
		return 0
	}

	// Sort by timestamp for chain reconstruction
	type indexed struct {
		event     audit.AuditEvent
		signature string
	}
	items := make([]indexed, len(events))
	for i, e := range events {
		items[i] = indexed{event: e, signature: signatures[i]}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].event.Timestamp.Equal(items[j].event.Timestamp) {
			return items[i].event.ID < items[j].event.ID
		}
		return items[i].event.Timestamp.Before(items[j].event.Timestamp)
	})

	signer := NewHMACChainSigner(key)
	breakCount := 0
	prevSig := ""

	for _, it := range items {
		expected, err := signer.Sign(it.event, prevSig)
		if err != nil || expected != it.signature {
			breakCount++
		}
		prevSig = it.signature
	}

	return breakCount
}

// buildRetentionCompliance checks retention policy against events.
func (g *ComplianceReportGenerator) buildRetentionCompliance(events []audit.AuditEvent, req ComplianceReportRequest) audit.RetentionComplianceSection {
	var rc audit.RetentionComplianceSection

	if g.retention == nil {
		rc.Compliant = true
		return rc
	}

	cfg := g.retention.Config()
	rc.PolicyEnabled = cfg.Enabled
	rc.DefaultDays = cfg.DefaultDays

	if !cfg.Enabled {
		rc.Compliant = true
		return rc
	}

	now := req.RetentionNow
	if now.IsZero() {
		now = time.Now()
	}

	days := g.retention.DaysForTenant(req.Scope.TenantID)
	cutoff := now.AddDate(0, 0, -days)

	var oldest time.Time
	for _, e := range events {
		if oldest.IsZero() || e.Timestamp.Before(oldest) {
			oldest = e.Timestamp
		}
		if e.Timestamp.After(cutoff) {
			rc.EventsInRange++
		} else {
			rc.EventsExpired++
		}
	}

	if !oldest.IsZero() {
		rc.OldestEvent = oldest
	}

	rc.Compliant = rc.EventsExpired == 0

	return rc
}

// buildTopErrors extracts the most frequent error patterns.
func buildTopErrors(events []audit.AuditEvent, maxN int) []audit.ErrorPattern {
	type errKey struct {
		msg    string
		source audit.Source
	}
	counts := make(map[errKey]int)

	for _, e := range events {
		if e.Error == "" {
			continue
		}
		k := errKey{msg: e.Error, source: e.Source}
		counts[k]++
	}

	// Sort by count descending
	keys := make([]errKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return counts[keys[i]] > counts[keys[j]]
	})

	n := len(keys)
	if n > maxN {
		n = maxN
	}

	patterns := make([]audit.ErrorPattern, 0, n)
	for i := 0; i < n; i++ {
		patterns = append(patterns, audit.ErrorPattern{
			Error:  keys[i].msg,
			Count:  counts[keys[i]],
			Source: keys[i].source,
		})
	}

	return patterns
}

// topN returns the top N entries from a count map, sorted by count descending.
func topN(counts map[string]int, maxN int) []audit.ActionCount {
	type kv struct {
		key   string
		count int
	}
	entries := make([]kv, 0, len(counts))
	for k, v := range counts {
		entries = append(entries, kv{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	n := len(entries)
	if n > maxN {
		n = maxN
	}

	result := make([]audit.ActionCount, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, audit.ActionCount{
			Action: entries[i].key,
			Count:  entries[i].count,
		})
	}
	return result
}
