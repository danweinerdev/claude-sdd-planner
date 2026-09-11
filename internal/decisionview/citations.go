package decisionview

import "fmt"

type CitationResult struct {
	Original  ResolvedDecision
	Effective *ResolvedDecision
}

func LookupReference(view *ResolvedView, ref string, legacy *LegacyContext) (CitationResult, error) {
	var result CitationResult
	if view == nil {
		return result, fmt.Errorf("decisionview: no resolved view")
	}
	id, err := citationIdentity(ref, legacy)
	if err != nil {
		return result, err
	}
	index := map[QualifiedID]ResolvedDecision{}
	for _, r := range view.Records {
		if _, duplicate := index[r.ID]; duplicate {
			return result, fmt.Errorf("decisionview: ambiguous historical identity %s", r.ID)
		}
		index[r.ID] = r
	}
	original, ok := index[id]
	if !ok {
		return result, fmt.Errorf("decisionview: unknown decision %s", id)
	}
	result.Original, err = copyResolvedDecision(original)
	if err != nil {
		return result, err
	}
	if view.Resolution != ResolutionComplete {
		return result, nil
	}
	seen := map[QualifiedID]bool{}
	current := original
	for {
		if seen[current.ID] {
			return result, fmt.Errorf("decisionview: cyclic effective replacement lineage")
		}
		seen[current.ID] = true
		if current.Applicability == "binding" {
			copy, err := copyResolvedDecision(current)
			if err != nil {
				return result, err
			}
			result.Effective = &copy
			return result, nil
		}
		if current.Applicability != "overridden" || current.Replacement == "" {
			return result, nil
		}
		next, ok := index[current.Replacement]
		if !ok {
			return result, fmt.Errorf("decisionview: missing effective replacement")
		}
		current = next
	}
}
func ValidateReferenceChange(before, after []string, legacy *LegacyContext) error {
	old := map[string]int{}
	allowed := map[string]int{}
	for _, ref := range before {
		old[ref]++
	}
	if legacy != nil {
		for _, id := range legacy.LocalIDs {
			allowed[id]++
		}
	}
	seen := map[string]int{}
	for _, ref := range after {
		if _, _, ok := ParseQualifiedID(ref); ok {
			continue
		}
		if _, err := citationIdentity(ref, legacy); err != nil {
			return err
		}
		seen[ref]++
		if seen[ref] > old[ref] || allowed[ref] == 0 {
			return fmt.Errorf("decisionview: newly introduced bare citation %s must be qualified", ref)
		}
	}
	return nil
}

func citationIdentity(ref string, legacy *LegacyContext) (QualifiedID, error) {
	if _, _, ok := ParseQualifiedID(ref); ok {
		return QualifiedID(ref), nil
	}
	if !localDecisionIDRe.MatchString(ref) || legacy == nil {
		return "", fmt.Errorf("decisionview: qualified reference or explicit legacy namespace required: %q", ref)
	}
	if err := legacy.Namespace.Validate(); err != nil {
		return "", err
	}
	for _, id := range legacy.LocalIDs {
		if id == ref {
			return QualifiedID("ledger:" + string(legacy.Namespace) + ":" + ref), nil
		}
	}
	return "", fmt.Errorf("decisionview: bare citation %s is not in the captured legacy inventory", ref)
}
func copyResolvedDecision(r ResolvedDecision) (ResolvedDecision, error) {
	entry, err := copyEntry(r.Original)
	if err != nil {
		return ResolvedDecision{}, err
	}
	r.Original = entry
	r.Lineage = append([]string(nil), r.Lineage...)
	r.Source.Archives = append([]string(nil), r.Source.Archives...)
	if r.Override != nil {
		copy := *r.Override
		copy.Basis.CanonicalContent = append([]byte(nil), copy.Basis.CanonicalContent...)
		copy.Basis.Lineage = append([]string(nil), copy.Basis.Lineage...)
		r.Override = &copy
	}
	return r, nil
}
