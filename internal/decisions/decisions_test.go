package decisions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIDForIsContentAddressedAndNormalized(t *testing.T) {
	a := IDFor("  Manifest reads   emit an\naudit event. ")
	b := IDFor("Manifest reads emit an audit event.")
	if a != b {
		t.Fatalf("normalization differs: %s vs %s", a, b)
	}
	if !IsID(a) {
		t.Fatalf("id %q is not well-formed", a)
	}
	if IDFor("a different statement") == a {
		t.Fatal("different statements share an id")
	}
}

func TestAppendCreatesFileAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "Plans", "P")
	path := PathFor(planDir)
	res, err := Append(path, "Reads are scoped to the caller's variants.", "", "", "2026-09-11", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Entry.Date != "2026-09-11" || !IsID(res.Entry.ID) {
		t.Fatalf("unexpected result %+v", res)
	}
	again, err := Append(path, "Reads are  scoped to the caller's variants.", "", "", "2026-09-12", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Entry.ID != res.Entry.ID || again.Entry.Date != "2026-09-11" {
		t.Fatalf("second append was not a no-op: %+v", again)
	}
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(raw), "]\n") {
		t.Fatalf("file is not canonical: %q", raw)
	}
}

func TestAppendConcurrentFirstWritersLoseNoSuccessfulDecision(t *testing.T) {
	const writers = 12
	for trial := 0; trial < 20; trial++ {
		path := PathFor(filepath.Join(t.TempDir(), "Plans", "P"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, writers)
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, err := Append(path, fmt.Sprintf("decision %d", i), "", "", "2026-01-01", "P", nil)
				results <- err
			}(i)
		}
		close(start)
		wg.Wait()
		close(results)

		succeeded := 0
		for err := range results {
			if err != nil {
				t.Errorf("trial %d: append failed: %v", trial, err)
				continue
			}
			succeeded++
		}
		entries, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != succeeded {
			t.Fatalf("trial %d: %d successful appends but %d entries persisted", trial, succeeded, len(entries))
		}
	}
}

func TestAppendTreatsNormalizedExistingStatementAsIdentical(t *testing.T) {
	path := PathFor(filepath.Join(t.TempDir(), "Plans", "P"))
	statement := "Keep   spacing\nnormalized."
	existing := Entry{ID: IDFor(statement), Date: "2026-01-01", Statement: statement}
	raw, err := Encode([]Entry{existing})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Append(path, "Keep spacing normalized.", "", "", "2026-01-02", "P", nil)
	if err != nil {
		t.Fatalf("normalized duplicate was treated as an id collision: %v", err)
	}
	if res.Created || res.Entry.ID != existing.ID {
		t.Fatalf("normalized duplicate was not a no-op: %+v", res)
	}
}

func TestAppendRefusesEmptyAndBadShapes(t *testing.T) {
	path := PathFor(filepath.Join(t.TempDir(), "Plans", "P"))
	if _, err := Append(path, "   ", "", "", "", "", nil); err == nil {
		t.Fatal("empty statement accepted")
	}
	if _, err := Append(path, "x", "D-0001", "", "", "", nil); err == nil {
		t.Fatal("bad supersedes accepted")
	}
	if _, err := Append(path, "x", "", "junk", "", "", nil); err == nil {
		t.Fatal("bad source accepted")
	}
	if _, err := Append(path, "x", "", "Designs/X:DD-4", "2026-01-01", "", nil); err != nil {
		t.Fatalf("design source refused: %v", err)
	}
	if _, err := Append(path, "y", "", "Reviews/2026-09-11-x.md:F-02", "2026-01-01", "", nil); err != nil {
		t.Fatalf("review source refused: %v", err)
	}
}

func TestSupersedeWithinPlan(t *testing.T) {
	path := PathFor(filepath.Join(t.TempDir(), "Plans", "P"))
	first, _ := Append(path, "Audit event carries both tenant ids.", "", "", "2026-01-01", "", nil)
	if _, err := Append(path, "Audit event carries only the caller tenant id.", "pd-00000000", "", "2026-01-02", "", nil); err == nil {
		t.Fatal("unknown supersedes accepted")
	}
	second, err := Append(path, "Audit event carries only the caller tenant id.", first.Entry.ID, "", "2026-01-02", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Append(path, "Audit event carries nothing.", first.Entry.ID, "", "2026-01-03", "", nil); err == nil {
		t.Fatal("superseding an already-superseded entry accepted")
	}
	if _, err := Append(path, "Self.", IDFor("Self."), "", "2026-01-03", "", nil); err == nil {
		t.Fatal("self-supersession accepted")
	}
	files, err := LoadRoot(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	if err != nil {
		t.Fatal(err)
	}
	x := NewIndex(files)
	cur := x.Current("")
	if len(cur) != 1 || cur[0].ID != second.Entry.ID {
		t.Fatalf("current = %+v", cur)
	}
	chain, ok := x.Lookup(first.Entry.ID, "P")
	if !ok || chain.SupersededBy == nil || chain.SupersededBy.Entry.ID != second.Entry.ID {
		t.Fatalf("lookup chain = %+v", chain)
	}
}

func TestCrossPlanSupersedeAndCollapse(t *testing.T) {
	root := t.TempDir()
	p := PathFor(filepath.Join(root, "Plans", "P"))
	q := PathFor(filepath.Join(root, "Plans", "Q"))
	shared := "Shared statement compiled into two plans."
	Append(p, shared, "", "Designs/X:DD-1", "2026-01-01", "", nil)
	Append(q, shared, "", "Designs/X:DD-1", "2026-01-01", "", nil)
	old, _ := Append(p, "P-only decision.", "", "", "2026-01-02", "", nil)

	files, _ := LoadRoot(root)
	x := NewIndex(files)
	cur := x.Current("")
	if len(cur) != 2 {
		t.Fatalf("expected collapse to 2 lines, got %d: %+v", len(cur), cur)
	}
	for _, c := range cur {
		if c.ID == IDFor(shared) && len(c.Plans) != 2 {
			t.Fatalf("shared id not collapsed across plans: %+v", c)
		}
	}
	// Bare reference to the shared id from an unrelated plan is ambiguous.
	if _, ok := x.Resolve(IDFor(shared), "R"); ok {
		t.Fatal("bare shared id resolved from a third plan")
	}
	if amb := x.Ambiguous(IDFor(shared), "R"); len(amb) != 2 {
		t.Fatalf("ambiguity = %v", amb)
	}
	// From P it resolves to P's copy.
	if hit, ok := x.Resolve(IDFor(shared), "P"); !ok || hit.Plan != "P" {
		t.Fatalf("resolve from P = %+v %v", hit, ok)
	}
	// Q supersedes P's decision by qualified id.
	if _, err := Append(q, "Q replaces the P-only decision.", "P:"+old.Entry.ID, "", "2026-01-03", "Q", x); err != nil {
		t.Fatal(err)
	}
	files, _ = LoadRoot(root)
	x = NewIndex(files)
	for _, c := range x.Current("") {
		if c.ID == old.Entry.ID {
			t.Fatal("cross-plan supersession did not retire the id")
		}
	}
	if _, ok := x.SuccessorOf(old.Entry.ID); !ok {
		t.Fatal("successor not found")
	}
}

func TestAppendRefusesCrossPlanIDCollisionWithoutChangingDestination(t *testing.T) {
	const existingStatement = "decision 1454"
	const collidingStatement = "decision 165661"
	if IDFor(existingStatement) != "pd-0c071537" || IDFor(collidingStatement) != "pd-0c071537" {
		t.Fatal("concrete SHA-256 prefix collision fixture changed")
	}

	root := t.TempDir()
	p := PathFor(filepath.Join(root, "Plans", "P"))
	q := PathFor(filepath.Join(root, "Plans", "Q"))
	if _, err := Append(p, existingStatement, "", "", "2026-01-01", "P", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Append(q, "Q's existing decision.", "", "", "2026-01-01", "Q", nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(q)
	if err != nil {
		t.Fatal(err)
	}
	files, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	index, err := ValidatedIndex(files)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Append(q, collidingStatement, "", "", "2026-01-02", "Q", index); err == nil {
		t.Fatal("cross-plan id collision was appended")
	} else if !strings.Contains(err.Error(), "pd-0c071537") || !strings.Contains(err.Error(), existingStatement) || !strings.Contains(err.Error(), collidingStatement) {
		t.Fatalf("collision error lacks actionable detail: %v", err)
	}
	after, err := os.ReadFile(q)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("destination changed on refused collision:\n--- before\n%s--- after\n%s", before, after)
	}
}

func TestValidatedIndexRefusesCrossPlanIDCollision(t *testing.T) {
	const first = "decision 1454"
	const second = "decision 165661"
	files := []PlanFile{
		{Plan: "P", Rel: "Plans/P/P-Decisions.json", Entries: []Entry{{ID: IDFor(first), Date: "2026-01-01", Statement: first}}},
		{Plan: "Q", Rel: "Plans/Q/Q-Decisions.json", Entries: []Entry{{ID: IDFor(second), Date: "2026-01-02", Statement: second}}},
	}
	if _, err := ValidatedIndex(files); err == nil {
		t.Fatal("validated index accepted distinct statements with the same id")
	} else if !strings.Contains(err.Error(), "pd-0c071537") || !strings.Contains(err.Error(), "P") || !strings.Contains(err.Error(), "Q") {
		t.Fatalf("collision error lacks plans and id: %v", err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	cases := map[string]string{
		"unknown field":    `[{"id":"pd-00000000","date":"2026-01-01","statement":"x","rationale":"no"}]`,
		"bad id":           `[{"id":"D-0001","date":"2026-01-01","statement":"x"}]`,
		"id mismatch":      `[{"id":"pd-00000000","date":"2026-01-01","statement":"x"}]`,
		"empty statement":  `[{"id":"` + IDFor("") + `","date":"2026-01-01","statement":""}]`,
		"bad date":         `[{"id":"` + IDFor("x") + `","date":"yesterday","statement":"x"}]`,
		"trailing content": `[] []`,
		"trailing bracket": `[]]`,
		"trailing brace":   `[]}`,
		"null":             `null`,
		"not an array":     `{}`,
	}
	for name, raw := range cases {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := `[{"id":"` + IDFor("x") + `","date":"2026-01-01","statement":"x"}]`
	if _, err := Decode([]byte(good)); err != nil {
		t.Fatalf("canonical refused: %v", err)
	}
}

func TestExplicitSupersessionQualifierIsHonored(t *testing.T) {
	path := PathFor(filepath.Join(t.TempDir(), "Plans", "P"))
	first, err := Append(path, "base", "", "", "2026-01-01", "P", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Append(path, "replacement", "Nonexistent:"+first.Entry.ID, "", "2026-01-02", "P", NewIndex(nil)); err == nil {
		t.Fatal("qualified supersedes resolved against the local file despite naming another plan")
	}
	if _, err := Append(path, "replacement", "P:"+first.Entry.ID, "", "2026-01-02", "P", nil); err != nil {
		t.Fatalf("matching-plan qualifier did not resolve locally: %v", err)
	}
}

func TestEncodeRoundTripsByteIdentical(t *testing.T) {
	entries := []Entry{{ID: IDFor("one"), Date: "2026-01-01", Statement: "one"}, {ID: IDFor("two"), Date: "2026-01-02", Statement: "two", Supersedes: IDFor("one"), Source: "Designs/X:DD-2"}}
	out, err := Encode(entries)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Encode(back)
	if string(out) != string(again) {
		t.Fatalf("not byte-identical:\n%s\n---\n%s", out, again)
	}
}

func TestLoadRootReportsMalformedFile(t *testing.T) {
	root := t.TempDir()
	bad := PathFor(filepath.Join(root, "Plans", "Bad"))
	os.MkdirAll(filepath.Dir(bad), 0o755)
	os.WriteFile(bad, []byte(`[{"id":"nope"}]`), 0o644)
	files, err := LoadRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Err == nil {
		t.Fatalf("malformed file not reported: %+v", files)
	}
	if got := NewIndex(files).Current(""); len(got) != 0 {
		t.Fatalf("malformed file contributed entries: %+v", got)
	}
}

func TestExtractDesignDecisionsBulletAndHeading(t *testing.T) {
	body := `## Design Decisions

- **DD-1**: First title.
  Context: a. Options: (a); (b).
  Decision: (b). Rationale: because.
- **DD-2**: Second title.
  Decision: x.

Trailing paragraph outside the list.

### DD-3 — Heading form
Context paragraph.

Decision paragraph.

## Error Handling
not a decision
`
	got := ExtractDesignDecisions(body)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d: %+v", len(got), got)
	}
	if got[0].ID != "DD-1" || !strings.HasPrefix(got[0].Text, "**DD-1**: First title.") || !strings.Contains(got[0].Text, "Rationale: because.") || strings.Contains(got[0].Text, "DD-2") {
		t.Fatalf("DD-1 text = %q", got[0].Text)
	}
	if got[1].ID != "DD-2" || strings.Contains(got[1].Text, "Trailing paragraph") {
		t.Fatalf("DD-2 text = %q", got[1].Text)
	}
	if got[2].ID != "DD-3" || !strings.HasPrefix(got[2].Text, "DD-3 — Heading form") || !strings.Contains(got[2].Text, "Decision paragraph.") || strings.Contains(got[2].Text, "Error Handling") {
		t.Fatalf("DD-3 text = %q", got[2].Text)
	}
}

func TestParseRef(t *testing.T) {
	id := IDFor("x")
	if p, i, ok := ParseRef(id); !ok || p != "" || i != id {
		t.Fatalf("bare: %q %q %v", p, i, ok)
	}
	if p, i, ok := ParseRef("CatalogPlane:" + id); !ok || p != "CatalogPlane" || i != id {
		t.Fatalf("qualified: %q %q %v", p, i, ok)
	}
	if _, _, ok := ParseRef("Designs/X:DD-1"); ok {
		t.Fatal("design ref parsed as decision ref")
	}
}

func TestSecondSuccessorRefusedRootWideAndConflictsReconcile(t *testing.T) {
	root := t.TempDir()
	q := PathFor(filepath.Join(root, "Plans", "Q"))
	p := PathFor(filepath.Join(root, "Plans", "P"))
	r := PathFor(filepath.Join(root, "Plans", "R"))
	base, _ := Append(q, "Base decision in Q.", "", "", "2026-01-01", "", nil)
	// P supersedes it.
	files, _ := LoadRoot(root)
	x := NewIndex(files)
	pSucc, err := Append(p, "P's replacement.", "Q:"+base.Entry.ID, "", "2026-01-02", "P", x)
	if err != nil {
		t.Fatal(err)
	}
	// R, working from a fresh index, is refused: the id already has a successor.
	files, _ = LoadRoot(root)
	x = NewIndex(files)
	if _, err := Append(r, "R's replacement.", "Q:"+base.Entry.ID, "", "2026-01-02", "R", x); err == nil {
		t.Fatal("second successor accepted with a current index")
	}
	// R on a stale branch — one where P's supersession has not merged
	// yet — sees Q but not P, so its append lands; the merge then
	// surfaces the conflict.
	var stale []PlanFile
	for _, f := range files {
		if f.Plan != "P" {
			stale = append(stale, f)
		}
	}
	rSucc, err := Append(r, "R's replacement.", "Q:"+base.Entry.ID, "", "2026-01-02", "R", NewIndex(stale))
	if err != nil {
		t.Fatal(err)
	}
	files, _ = LoadRoot(root)
	x = NewIndex(files)
	conflicts := x.Conflicts()
	if len(conflicts) != 1 || conflicts[0].ID != base.Entry.ID || len(conflicts[0].Successors) != 2 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	// Reconcile: one entry superseding both competing successors.
	if _, err := Append(q, "Reconciled replacement.", "P:"+pSucc.Entry.ID+", R:"+rSucc.Entry.ID, "", "2026-01-03", "Q", x); err != nil {
		t.Fatal(err)
	}
	files, _ = LoadRoot(root)
	x = NewIndex(files)
	if got := x.Conflicts(); len(got) != 0 {
		t.Fatalf("conflict survived reconciliation: %+v", got)
	}
	cur := x.Current("")
	if len(cur) != 1 || cur[0].Statement != "Reconciled replacement." {
		t.Fatalf("current = %+v", cur)
	}
}

func TestConflictsFollowLiveDescendantBranchesUntilConvergence(t *testing.T) {
	mk := func(statement, supersedes string) Entry {
		return Entry{ID: IDFor(statement), Date: "2026-01-01", Statement: statement, Supersedes: supersedes}
	}
	base := mk("base", "")
	b := mk("branch B", base.ID)
	c := mk("branch C", base.ID)
	d := mk("branch B update", b.ID)
	x := NewIndex([]PlanFile{
		{Plan: "A", Entries: []Entry{base}},
		{Plan: "B", Entries: []Entry{b, d}},
		{Plan: "C", Entries: []Entry{c}},
	})
	conflicts := x.Conflicts()
	if len(conflicts) != 1 || conflicts[0].ID != base.ID || len(conflicts[0].Successors) != 2 {
		t.Fatalf("live descendant fork was not reported: %+v", conflicts)
	}
	want := map[string]bool{c.ID: true, d.ID: true}
	for _, successor := range conflicts[0].Successors {
		delete(want, successor.Entry.ID)
	}
	if len(want) != 0 {
		t.Fatalf("conflict did not name both live descendants: %+v", conflicts[0].Successors)
	}

	reconciled := mk("converged", "B:"+d.ID+", C:"+c.ID)
	x = NewIndex([]PlanFile{
		{Plan: "A", Entries: []Entry{base}},
		{Plan: "B", Entries: []Entry{b, d}},
		{Plan: "C", Entries: []Entry{c}},
		{Plan: "R", Entries: []Entry{reconciled}},
	})
	if conflicts := x.Conflicts(); len(conflicts) != 0 {
		t.Fatalf("conflict survived actual branch convergence: %+v", conflicts)
	}
}

func TestDuplicateSuccessorIdentityAcrossPlansIsNotAConflict(t *testing.T) {
	base := Entry{ID: IDFor("base"), Date: "2026-01-01", Statement: "base"}
	successor := Entry{ID: IDFor("replacement"), Date: "2026-01-02", Statement: "replacement", Supersedes: base.ID, Source: "Designs/X:DD-2"}
	x, err := ValidatedIndex([]PlanFile{
		{Plan: "A", Entries: []Entry{base}},
		{Plan: "P", Entries: []Entry{successor}},
		{Plan: "Q", Entries: []Entry{successor}},
	})
	if err != nil {
		t.Fatalf("identical successor copies were rejected: %v", err)
	}
	if conflicts := x.Conflicts(); len(conflicts) != 0 {
		t.Fatalf("identical successor copies were treated as competing branches: %+v", conflicts)
	}
}

func TestReconciledForkIsNotRevivedByLaterFork(t *testing.T) {
	mk := func(statement, supersedes string) Entry {
		return Entry{ID: IDFor(statement), Date: "2026-01-01", Statement: statement, Supersedes: supersedes}
	}
	base := mk("base", "")
	left := mk("left", base.ID)
	right := mk("right", base.ID)
	joined := mk("joined", "Left:"+left.ID+", Right:"+right.ID)
	one := mk("later one", joined.ID)
	two := mk("later two", joined.ID)
	x := NewIndex([]PlanFile{
		{Plan: "Base", Entries: []Entry{base}},
		{Plan: "Left", Entries: []Entry{left}},
		{Plan: "Right", Entries: []Entry{right}},
		{Plan: "Joined", Entries: []Entry{joined}},
		{Plan: "One", Entries: []Entry{one}},
		{Plan: "Two", Entries: []Entry{two}},
	})
	conflicts := x.Conflicts()
	if len(conflicts) != 1 || conflicts[0].ID != joined.ID {
		t.Fatalf("later fork should be reported only at its origin: %+v", conflicts)
	}
}

func TestConflictsHandlesTwentyFiveStageReconciledDiamondHistory(t *testing.T) {
	mk := func(statement, supersedes string) Entry {
		return Entry{ID: IDFor(statement), Date: "2026-01-01", Statement: statement, Supersedes: supersedes}
	}
	base := mk("diamond base", "")
	entries := []Entry{base}
	previous := base
	for stage := 1; stage <= 25; stage++ {
		left := mk(fmt.Sprintf("diamond stage %d left", stage), previous.ID)
		right := mk(fmt.Sprintf("diamond stage %d right", stage), previous.ID)
		joined := mk(fmt.Sprintf("diamond stage %d joined", stage), left.ID+", "+right.ID)
		entries = append(entries, left, right, joined)
		previous = joined
	}

	x := NewIndex([]PlanFile{{Plan: "History", Entries: entries}})
	if conflicts := x.Conflicts(); len(conflicts) != 0 {
		t.Fatalf("fully reconciled diamond history has conflicts: %+v", conflicts)
	}
}

func TestConflictsHandlesSupersessionCycle(t *testing.T) {
	base := Entry{ID: IDFor("cycle base"), Date: "2026-01-01", Statement: "cycle base"}
	a := Entry{ID: IDFor("cycle a"), Date: "2026-01-01", Statement: "cycle a"}
	b := Entry{ID: IDFor("cycle b"), Date: "2026-01-01", Statement: "cycle b"}
	a.Supersedes = base.ID + ", " + b.ID
	b.Supersedes = base.ID + ", " + a.ID
	x := NewIndex([]PlanFile{{Plan: "Cycle", Entries: []Entry{base, a, b}}})
	if conflicts := x.Conflicts(); len(conflicts) != 0 {
		t.Fatalf("converged cycle was reported as a conflict: %+v", conflicts)
	}
}

func TestExtractDesignDecisionsSupersedesClause(t *testing.T) {
	body := "- **DD-1**: Old.\n  Decision: a.\n- **DD-2**: New.\n  Supersedes DD-1. Decision: b.\n- **DD-3**: Other.\n  Supersedes: Other:DD-4 because reasons.\n"
	got := ExtractDesignDecisions(body)
	if got[0].Supersedes != "" || got[1].Supersedes != "DD-1" || got[2].Supersedes != "Other:DD-4" {
		t.Fatalf("supersedes = %q %q %q", got[0].Supersedes, got[1].Supersedes, got[2].Supersedes)
	}
}
