package decisionview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ScopeContext struct {
	Roots          Roots
	Owner          OwnerID
	Related        map[string][]string
	ArtifactOwners map[string]OwnerID
}

func ScopesOverlap(left, right []string, related map[string][]string) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	adj := map[string]map[string]bool{}
	for a, list := range related {
		a = scopeKey(a)
		if adj[a] == nil {
			adj[a] = map[string]bool{}
		}
		for _, b := range list {
			b = scopeKey(b)
			if adj[b] == nil {
				adj[b] = map[string]bool{}
			}
			adj[a][b] = true
			adj[b][a] = true
		}
	}
	for _, a := range left {
		for _, b := range right {
			if !validScopePath(a) || !validScopePath(b) {
				return true
			}
			a, b = scopeKey(a), scopeKey(b)
			if nestedScope(a, b) {
				return true
			}
			frontier := map[string]bool{a: true}
			for node := range adj {
				if nestedScope(a, node) {
					frontier[node] = true
				}
			}
			for depth := 0; depth < 2; depth++ {
				next := map[string]bool{}
				for node := range frontier {
					for linked := range adj[node] {
						if nestedScope(linked, b) {
							return true
						}
						next[linked] = true
					}
				}
				frontier = next
			}
		}
	}
	return false
}
func CheckScopes(view *ResolvedView, context ScopeContext) []Diagnostic {
	var out []Diagnostic
	issue := func(id QualifiedID, scope, why string) {
		out = append(out, Diagnostic{Code: "FDL030", Severity: Error, Path: scope, Message: fmt.Sprintf("%s: %s", id, why), Correction: "Reconcile the scope or provide an explicitly scoped local replacement; do not treat it as irrelevant."})
	}
	if view == nil || context.Owner.Validate() != nil || view.OwnerID != context.Owner {
		issue("", "", "scope context belongs to a different or unknown represented repository")
		return out
	}
	for _, record := range view.Records {
		if record.Applicability != "binding" && record.Applicability != "unresolved" {
			continue
		}
		raw, present := record.Original["scope"]
		if !present {
			continue
		}
		list, ok := scopeStrings(raw)
		if !ok {
			issue(record.ID, "", "scope must be an explicit string list")
			continue
		}
		for _, scope := range list {
			if !validScopePath(scope) {
				issue(record.ID, scope, "scope is not a safe relative path")
				continue
			}
			base := context.Roots.Repository
			if planningScope(scope) {
				base = context.Roots.Planning
				owner, known := scopeOwner(scope, context.ArtifactOwners)
				if !known && rootsContain(context.Roots.Repository, context.Roots.Planning) {
					owner = context.Owner
					known = true
				}
				if !known || owner != context.Owner {
					issue(record.ID, scope, "external planning scope ownership is unknown or foreign")
					continue
				}
			}
			exists, err := scopeExists(base, scope)
			if err != nil {
				issue(record.ID, scope, "scope could not be checked: "+err.Error())
				out[len(out)-1].Severity = Operational
			} else if !exists {
				issue(record.ID, scope, "scope is missing from its declared root")
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func scopeStrings(v any) ([]string, bool) {
	switch list := v.(type) {
	case []string:
		return list, true
	case []any:
		out := make([]string, 0, len(list))
		for _, x := range list {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}
func validScopePath(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && validateRelativeLocator(strings.TrimSuffix(s, "/")) == nil
}
func scopeKey(s string) string {
	s = strings.TrimSuffix(s, "/")
	if planningScope(s) {
		s = strings.TrimSuffix(s, "/README.md")
		s = strings.TrimSuffix(s, ".md")
	}
	return s
}
func nestedScope(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func planningScope(p string) bool {
	head := strings.SplitN(p, "/", 2)[0]
	switch head {
	case "Research", "Brainstorm", "Specs", "Designs", "Plans", "Decisions", "Retro", "Diagrams":
		return true
	}
	return false
}
func scopeOwner(scope string, owners map[string]OwnerID) (OwnerID, bool) {
	key := scopeKey(scope)
	best := ""
	var owner OwnerID
	keys := make([]string, 0, len(owners))
	for k := range owners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, original := range keys {
		candidate, id := scopeKey(original), owners[original]
		if key != candidate && !strings.HasPrefix(key, candidate+"/") {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
			owner = id
		} else if len(candidate) == len(best) && owner != id {
			owner = ""
		}
	}
	return owner, best != ""
}
func rootsContain(repository, planning string) bool {
	if repository == "" || planning == "" {
		return false
	}
	a, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return false
	}
	b, err := filepath.EvalSymlinks(planning)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func scopeExists(base, scope string) (bool, error) {
	if base == "" {
		return false, fmt.Errorf("declared scope root unavailable")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return false, err
	}
	defer root.Close()
	scope = strings.TrimSuffix(scope, "/")
	candidates := []string{scope}
	if planningScope(scope) && !strings.HasSuffix(scope, ".md") {
		candidates = append(candidates, scope+".md", scope+"/README.md")
	}
	for _, p := range candidates {
		if _, err := root.Stat(filepath.FromSlash(p)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}
