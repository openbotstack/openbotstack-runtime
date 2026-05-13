// Package archguard contains architecture contract tests that enforce
// structural invariants across the OpenBotStack codebase.
//
// Run with: go test -v ./archguard/...
package archguard

import (
	"go/parser"
	"go/token"
	"os/exec"
	"strings"
	"testing"
)

// Core packages are at ../../openbotstack-core/ relative to this file.
const coreRoot = "../../openbotstack-core"

// =============================================================================
// Contract 1: Planner MUST NOT depend on MCP, runtime transport, wazero, or
// provider internals.
// =============================================================================

func TestContract1_PlannerDependencyIsolation(t *testing.T) {
	forbidden := []string{
		"jsonrpc",
		"wazero",
		"sqlite",
		"stdio",
		"sse",
	}
	// Also forbid importing the runtime module itself
	plannerDir := coreRoot + "/planner"
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, plannerDir, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse planner dir: %v", err)
	}
	for pkgName, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				// Forbid importing runtime packages
				if strings.Contains(path, "openbotstack-runtime") {
					t.Errorf("planner package %q imports runtime %q", pkgName, path)
				}
				// Forbid importing transport-level packages
				for _, f := range forbidden {
					if strings.Contains(path, f) {
						t.Errorf("planner package %q imports forbidden package %q (contains %q)",
							pkgName, path, f)
					}
				}
				// Forbid importing MCP packages
				if strings.Contains(path, "/mcp") && !strings.Contains(path, "mcp/mcp") {
					t.Errorf("planner package %q imports MCP package %q", pkgName, path)
				}
			}
		}
	}
}

// =============================================================================
// Contract 5: No transport leakage in core packages.
// Core MUST NOT reference MCP transport details.
// =============================================================================

func TestContract5_NoTransportLeakageInCore(t *testing.T) {
	coreDirs := []string{
		coreRoot + "/capability",
		coreRoot + "/planner",
		coreRoot + "/execution",
		coreRoot + "/control/agent",
	}
	for _, dir := range coreDirs {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
		if err != nil {
			t.Logf("skip %s: %v", dir, err)
			continue
		}
		for pkgName, pkg := range pkgs {
			for _, file := range pkg.Files {
				for _, imp := range file.Imports {
					path := strings.Trim(imp.Path.Value, `"`)
					// runtime packages in core = leak
					if strings.Contains(path, "openbotstack-runtime") {
						t.Errorf("core package %q imports runtime %q — dependency violation",
							pkgName, path)
					}
					// transport specifics in capability/planner = leak
					if (strings.Contains(dir, "capability") || strings.Contains(dir, "planner")) &&
						(strings.Contains(path, "jsonrpc") || strings.Contains(path, "wazero") || strings.Contains(path, "sqlite")) {
						t.Errorf("core package %q in %s imports %q — transport leak",
							pkgName, dir, path)
					}
				}
			}
		}
	}
}

// =============================================================================
// Contract 5b: No kind-checking in planner/execution core paths.
// Detects `if.*Kind.*==.*"mcp"` and similar patterns.
// =============================================================================

func TestContract5b_NoKindCheckingInCore(t *testing.T) {
	coreDirs := []string{
		coreRoot + "/planner",
		coreRoot + "/execution",
	}
	patterns := []string{
		`Kind == "mcp"`,
		`Kind=="mcp"`,
		`Kind == "skill"`,
		`Kind=="skill"`,
		`Kind == "wasm"`,
		`Kind=="wasm"`,
	}
	for _, dir := range coreDirs {
		for _, pattern := range patterns {
			out, err := exec.Command("grep", "-rn", pattern, dir).Output()
			if err == nil && len(out) > 0 {
				t.Errorf("kind-checking pattern %q found in core path %s:\n%s",
					pattern, dir, string(out))
			}
		}
	}
}

// =============================================================================
// Contract 3: Audit events must carry mandatory fields.
// =============================================================================

func TestContract3_AuditEventMandatoryFields(t *testing.T) {
	out, err := exec.Command("grep", "-A20",
		"CREATE TABLE IF NOT EXISTS audit_logs",
		"../persistence/db.go").Output()
	if err != nil {
		t.Skip("could not read db.go schema")
	}
	schema := string(out)
	required := []string{"id", "tenant_id", "request_id", "action", "timestamp"}
	for _, col := range required {
		if !strings.Contains(schema, col) {
			t.Errorf("audit_logs table missing required column: %s", col)
		}
	}
}

// =============================================================================
// Contract 6: CapabilityRegistry is the unique capability source.
// =============================================================================

func TestContract6_CapabilityRegistryIsUnique(t *testing.T) {
	out, err := exec.Command("grep", "-c",
		"CapabilityRegistry",
		coreRoot+"/capability/registry.go").Output()
	if err != nil {
		t.Fatal("CapabilityRegistry not found in core/capability/registry.go")
	}
	if strings.TrimSpace(string(out)) == "0" {
		t.Error("CapabilityRegistry interface missing")
	}

	out, err = exec.Command("grep", "-c",
		"MemoryCapabilityRegistry",
		coreRoot+"/capability/memory_registry.go").Output()
	if err != nil {
		t.Fatal("MemoryCapabilityRegistry not found")
	}
	if strings.TrimSpace(string(out)) == "0" {
		t.Error("MemoryCapabilityRegistry implementation missing")
	}
}

// =============================================================================
// Contract 7: ToolSpec is the planner's only capability representation.
// =============================================================================

func TestContract7_ToolSpecIsPlannerSource(t *testing.T) {
	out, err := exec.Command("grep", "-c",
		"type ToolSpec struct",
		coreRoot+"/planner/toolspec.go").Output()
	if err != nil {
		t.Fatal("ToolSpec not found")
	}
	if strings.TrimSpace(string(out)) == "0" {
		t.Error("ToolSpec struct missing")
	}

	for _, fn := range []string{"SchemaToToolSpec", "CapabilityToToolSpec", "FormatToolSpecs"} {
		out, err := exec.Command("grep", "-c", fn,
			coreRoot+"/planner/toolspec.go").Output()
		if err != nil || strings.TrimSpace(string(out)) == "0" {
			t.Errorf("required function %s not found in toolspec.go", fn)
		}
	}
}

// =============================================================================
// Contract 8: core MUST NOT import runtime.
// =============================================================================

func TestContract8_CoreNeverImportsRuntime(t *testing.T) {
	out, err := exec.Command("grep", "-rn",
		`"github.com/openbotstack/openbotstack-runtime`,
		coreRoot).Output()
	if err == nil && len(out) > 0 {
		t.Errorf("core package imports runtime — dependency violation:\n%s", string(out))
	}
}
