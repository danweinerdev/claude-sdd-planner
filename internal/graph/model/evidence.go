package model

// ValidateEvidenceGate validates type-specific gate declarations.
func ValidateEvidenceGate(n *Node) []string {
	if n == nil {
		return nil
	}
	g := &n.Gate
	var out []string
	if g.Type != GateTests {
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
	return out
}
