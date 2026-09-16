package model

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxExecutionTimeoutSeconds leaves room for the owned process cleanup
// budget after converting whole seconds to time.Duration. It is a wire-level
// bound; 32-bit decoders additionally reject values that cannot fit int.
const MaxExecutionTimeoutSeconds int64 = 9223372031

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateEvidenceGate validates the declaration-only observed evidence
// contract. Filesystem resolution remains the compiler's responsibility.
func ValidateEvidenceGate(n *Node) []string {
	if n == nil {
		return nil
	}
	g := &n.Gate
	var out []string
	if g.Evidence != "" && g.Evidence != EvidenceLegacy && g.Evidence != EvidenceObservedV1 && g.Evidence != EvidenceReportedV1 {
		out = append(out, fmt.Sprintf("unknown evidence value %q", g.Evidence))
	}
	if g.Type != GateTests {
		if g.Evidence != "" {
			out = append(out, "evidence is allowed only on tests gates")
		}
		if g.Execution != nil {
			out = append(out, "execution is allowed only on observed-v1 tests gates")
		}
		if g.Report != nil {
			out = append(out, "report is allowed only on reported-v1 tests gates")
		}
		if len(g.Tests) != 0 {
			out = append(out, "tests are allowed only on tests gates")
		}
		if g.Type != GateCommand && g.Command != "" {
			out = append(out, "command is allowed only on command gates")
		}
		if g.Type != GateReview && g.Lanes != nil {
			out = append(out, "lanes are allowed only on review gates")
		}
		return out
	}
	if g.Command != "" {
		out = append(out, "command is allowed only on command gates")
	}
	if g.Lanes != nil {
		out = append(out, "lanes are allowed only on review gates")
	}
	if g.Evidence == EvidenceReportedV1 {
		if g.Execution != nil {
			out = append(out, "execution is forbidden on reported-v1 tests gates")
		}
		if g.Report == nil {
			return append(out, "reported-v1 tests gate requires report")
		}
		if len(g.Tests) == 0 {
			out = append(out, "reported-v1 tests gate requires at least one test")
		}
		if g.Report.Format != "go-test-json-v1" {
			out = append(out, "report format must be \"go-test-json-v1\"")
		}
		if strings.TrimSpace(g.Report.Runner) == "" {
			out = append(out, "report runner must be a nonempty logical identity")
		}
		if g.Report.EnvironmentKeys == nil {
			out = append(out, "report environment_keys must be an explicit array")
		}
		if g.Report.TestSupportInputs == nil {
			out = append(out, "report test_support_inputs must be an explicit array")
		}
		if g.Report.TestSupportArtifacts == nil {
			out = append(out, "report test_support_artifacts must be an explicit array")
		}
		for _, path := range n.Artifacts {
			if obviousDirectoryPath(path) {
				out = append(out, fmt.Sprintf("reported-v1 artifact %q must name a file, not a directory", path))
			}
		}
		for _, t := range g.Tests {
			if strings.TrimSpace(t.Package) == "" {
				out = append(out, fmt.Sprintf("test %q requires package", t.ID))
			}
			if obviousDirectoryPath(t.File) {
				out = append(out, fmt.Sprintf("test file %q must name a file, not a directory", t.File))
			}
		}
		out = append(out, validateReportMembership(n, g.Report)...)
		return out
	}
	if g.Report != nil {
		out = append(out, "report is forbidden unless evidence is reported-v1")
	}
	if g.Evidence != EvidenceObservedV1 {
		if g.Execution != nil {
			out = append(out, "execution is forbidden on legacy tests gates")
		}
		return out
	}
	if len(g.Tests) == 0 {
		out = append(out, "observed-v1 tests gate requires at least one test")
	}
	if g.Execution == nil {
		return append(out, "observed-v1 tests gate requires execution")
	}
	p := g.Execution
	if p.Adapter != "go-test-v1" {
		out = append(out, fmt.Sprintf("execution adapter must be %q", "go-test-v1"))
	}
	if p.TimeoutSeconds <= 0 {
		out = append(out, "execution timeout_seconds must be positive and finite")
	} else if int64(p.TimeoutSeconds) > MaxExecutionTimeoutSeconds {
		out = append(out, fmt.Sprintf("execution timeout_seconds must be representable with cleanup (maximum %d)", MaxExecutionTimeoutSeconds))
	}
	seenFlags := map[string]bool{}
	for i := 0; i < len(p.Args); i++ {
		a := p.Args[i]
		flag := a
		if eq := strings.IndexByte(flag, '='); eq >= 0 {
			flag = flag[:eq]
		}
		if seenFlags[flag] {
			out = append(out, fmt.Sprintf("execution args repeat flag %q", flag))
		}
		seenFlags[flag] = true
		switch flag {
		case "-race", "-short":
			if a != flag {
				out = append(out, fmt.Sprintf("execution arg %q does not take a value", flag))
			}
		case "-tags":
			if a == "-tags" {
				if i+1 >= len(p.Args) || strings.TrimSpace(p.Args[i+1]) == "" || strings.HasPrefix(p.Args[i+1], "-") {
					out = append(out, "execution arg -tags requires an explicit value")
				} else {
					i++
				}
			} else if strings.TrimSpace(strings.TrimPrefix(a, "-tags=")) == "" {
				out = append(out, "execution arg -tags requires an explicit value")
			}
		default:
			out = append(out, fmt.Sprintf("execution arg %q is not allowed", a))
		}
	}
	seenEnvironment := map[string]bool{}
	for _, key := range p.EnvironmentKeys {
		folded := strings.ToUpper(key)
		if !environmentKeyPattern.MatchString(key) {
			out = append(out, fmt.Sprintf("environment_keys entry %q is not a valid environment name", key))
		}
		if seenEnvironment[folded] {
			out = append(out, fmt.Sprintf("environment_keys contains duplicate %q", key))
		}
		seenEnvironment[folded] = true
	}
	inputKeys := map[string]bool{}
	wholeRepoInputs := map[string]bool{}
	for _, in := range n.Inputs {
		inputKeys[InputKey(in)] = true
		if in.Root == InputRootRepository && in.Section == nil {
			wholeRepoInputs[in.Path] = true
		}
	}
	artifacts := map[string]bool{}
	for _, a := range n.Artifacts {
		artifacts[a] = true
	}
	seenSupportInputs := map[string]bool{}
	for _, key := range p.TestSupportInputs {
		if strings.TrimSpace(key) == "" {
			out = append(out, "test_support_inputs contains an empty key")
		}
		if seenSupportInputs[key] {
			out = append(out, fmt.Sprintf("test_support_inputs contains duplicate %q", key))
		}
		seenSupportInputs[key] = true
		if !inputKeys[key] {
			out = append(out, fmt.Sprintf("test_support_inputs key %q is not declared in inputs", key))
		}
	}
	seenSupportArtifacts := map[string]bool{}
	for _, path := range p.TestSupportArtifacts {
		if strings.TrimSpace(path) == "" {
			out = append(out, "test_support_artifacts contains an empty path")
		}
		if seenSupportArtifacts[path] {
			out = append(out, fmt.Sprintf("test_support_artifacts contains duplicate %q", path))
		}
		seenSupportArtifacts[path] = true
		if !artifacts[path] {
			out = append(out, fmt.Sprintf("test_support_artifacts path %q is not declared in artifacts", path))
		}
	}
	for _, test := range g.Tests {
		if !artifacts[test.File] && !wholeRepoInputs[test.File] {
			out = append(out, fmt.Sprintf("test file %q must be a declared artifact or whole repository input", test.File))
		}
	}
	return out
}

func validateReportMembership(n *Node, p *ReportProfile) []string {
	var out []string
	seen := map[string]bool{}
	for _, key := range p.EnvironmentKeys {
		folded := strings.ToUpper(key)
		if !environmentKeyPattern.MatchString(key) {
			out = append(out, fmt.Sprintf("environment_keys entry %q is not a valid environment name", key))
		}
		if seen[folded] {
			out = append(out, fmt.Sprintf("environment_keys contains duplicate %q", key))
		}
		seen[folded] = true
	}
	inputs, artifacts := map[string]bool{}, map[string]bool{}
	for _, in := range n.Inputs {
		inputs[InputKey(in)] = true
	}
	for _, a := range n.Artifacts {
		artifacts[a] = true
	}
	seenInputs := map[string]bool{}
	for _, key := range p.TestSupportInputs {
		if strings.TrimSpace(key) == "" {
			out = append(out, "test_support_inputs contains an empty key")
		}
		if seenInputs[key] {
			out = append(out, fmt.Sprintf("test_support_inputs contains duplicate %q", key))
		}
		seenInputs[key] = true
		if !inputs[key] {
			out = append(out, fmt.Sprintf("test_support_inputs key %q is not declared in inputs", key))
		}
	}
	seenArtifacts := map[string]bool{}
	for _, path := range p.TestSupportArtifacts {
		if strings.TrimSpace(path) == "" {
			out = append(out, "test_support_artifacts contains an empty path")
		}
		if seenArtifacts[path] {
			out = append(out, fmt.Sprintf("test_support_artifacts contains duplicate %q", path))
		}
		seenArtifacts[path] = true
		if obviousDirectoryPath(path) {
			out = append(out, fmt.Sprintf("test_support_artifacts path %q must name a file, not a directory", path))
		}
		if !artifacts[path] {
			out = append(out, fmt.Sprintf("test_support_artifacts path %q is not declared in artifacts", path))
		}
	}
	whole := map[string]bool{}
	for _, in := range n.Inputs {
		if in.Root == InputRootRepository && in.Section == nil {
			whole[in.Path] = true
		}
	}
	for _, t := range n.Gate.Tests {
		if !artifacts[t.File] && !whole[t.File] {
			out = append(out, fmt.Sprintf("test file %q must be a declared artifact or whole repository input", t.File))
		}
	}
	return out
}

func obviousDirectoryPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == "." || path == ".." || strings.HasSuffix(path, "/") || strings.HasSuffix(path, `\`)
}
