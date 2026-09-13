package rules

// Family: artifact supersession — the spec/design/plan analogue of the review
// rules (SDD099-SDD102).
//
// Supersession already had teeth for decisions (SDD121 refuses a live artifact
// citing a superseded decision) and for reviews. Specs, designs, and plans had
// none: an artifact could carry `superseded_by` while still claiming
// `status: approved`, and a live plan could build against a design that had
// been replaced, with nothing said. That is the failure this family closes —
// work planned against retired intent is the expensive kind of drift, because
// it looks correct right up until it ships.

// supersedableKinds are the artifact kinds this family governs. Reviews and
// decision logs are excluded: they have their own supersession rules whose
// semantics (frozen review identity) differ.
var supersedableKinds = map[string]bool{
	"spec": true, "design": true, "plan": true,
}

// isSupersededArtifact reports whether an artifact is retired — either by
// status or by carrying a successor link.
func isSupersededArtifact(a *Artifact) bool {
	if a.Meta == nil {
		return false
	}
	return a.Status() == "superseded" || metaStr(a.Meta, "superseded_by") != ""
}

func init() {
	Register(&Rule{
		Code: "SDD178", Severity: Error, PyFunc: "_supersession",
		What: "a spec/design/plan's `superseded_by` and its status disagree",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.Meta == nil || !supersedableKinds[a.Kind()] {
				return
			}
			status := a.Status()
			supersededBy := metaStr(a.Meta, "superseded_by")
			if status == "superseded" && supersededBy == "" {
				emit(Diagnostic{
					Code: "SDD178", Severity: Error, Path: a.Rel, Line: 1,
					Message:    "Superseded " + a.Kind() + " lacks `superseded_by`.",
					Correction: "Link the replacing artifact, or use `sdd " + a.Kind() + " supersede <path> --by <successor>`.",
				})
			}
			if supersededBy != "" && status != "superseded" {
				emit(Diagnostic{
					Code: "SDD178", Severity: Error, Path: a.Rel, Line: 1,
					Message: "`" + a.Kind() + "` with `superseded_by` has status `" + status +
						"`; a replaced artifact is not still " + status + ".",
					Correction: "Run `sdd " + a.Kind() + " supersede <path> --by <successor>` so status and the link agree.",
				})
			}
		},
		Bad: []Example{{Name: "superseded-by-without-status", Files: map[string]string{
			"Designs/Sample/README.md": replaceFirst(validDesign("Text."),
				"related: []", "related: []\nsuperseded_by: \"Designs/Newer\""),
		}}},
		Good: []Example{{Name: "live-design", Files: map[string]string{
			"Designs/Sample/README.md": validDesign("Text."),
		}}},
	})

	Register(&Rule{
		Code: "SDD179", Severity: Error, PyFunc: "_supersession",
		What: "a live artifact relates to a superseded spec/design/plan",
		CheckRoot: func(r *Root, emit func(Diagnostic)) {
			for _, a := range r.Artifacts {
				// Only LIVE artifacts are held to this. A superseded design
				// naturally still references its own era's artifacts, and a
				// debrief or retro is a historical record by definition.
				if a.Meta == nil || !isLiveArtifact(a) || isSupersededArtifact(a) || reviewOfRetiredPlan(r, a) {
					continue
				}
				related, ok := a.Meta["related"].([]any)
				if !ok {
					continue
				}
				for _, ref := range related {
					s, ok := ref.(string)
					if !ok {
						continue
					}
					target := resolveRef(r, s)
					if target == nil || !supersedableKinds[target.Kind()] {
						continue
					}
					if !isSupersededArtifact(target) {
						continue
					}
					successor := metaStr(target.Meta, "superseded_by")
					fix := "Relate the replacement instead"
					if successor != "" {
						fix = "Relate `" + successor + "` instead"
					}
					emit(Diagnostic{
						Code: "SDD179", Severity: Error, Path: a.Rel, Line: a.Line(s, false),
						Message: "Live `" + a.Kind() + "` relates to superseded " + target.Kind() +
							" `" + target.Rel + "`.",
						Correction: fix + ", or mark this artifact superseded too if it is retired work.",
					})
				}
			}
		},
		Bad: []Example{{Name: "live-plan-relates-superseded-design", Files: map[string]string{
			"Plans/Sample/README.md": replaceFirst(validPlan(false),
				"related: []", "related: [Designs/Old]"),
			"Designs/Old/README.md": replaceFirst(
				replaceFirst(validDesign("Text."), "status: draft", "status: superseded"),
				"related: []", "related: []\nsuperseded_by: \"Designs/New\""),
		}}},
		Good: []Example{{Name: "live-plan-relates-live-design", Files: map[string]string{
			"Plans/Sample/README.md": replaceFirst(validPlan(false), "related: []", "related: [Designs/Live]"),
			"Designs/Live/README.md": validDesign("Text."),
		}}},
	})
}

// reviewOfRetiredPlan reports whether a review artifact reviews a document
// of a plan that is archived or complete. Such a review is frozen history:
// it related to the specs and designs that governed the work when it was
// done, and retiring those later does not make the review "live" work that
// must re-relate.
func reviewOfRetiredPlan(r *Root, a *Artifact) bool {
	if a.Kind() != "review" {
		return false
	}
	ref, _ := a.Meta["review_of"].(string)
	target := resolveRef(r, ref)
	if target == nil {
		return false
	}
	plan := target
	if target.Kind() == "phase" {
		if name := metaStr(target.Meta, "plan"); name != "" {
			if p, ok := r.ByPath["Plans/"+name+"/README.md"]; ok {
				plan = p
			}
		}
	}
	if plan.Kind() != "plan" {
		return false
	}
	switch plan.Status() {
	case "archived", "complete":
		return true
	}
	return false
}
