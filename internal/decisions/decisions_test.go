package decisions

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDecodeIsStrict(t *testing.T) {
	cases := map[string]string{
		"unknown field":    `[{"id":"pd-00000000","date":"2026-01-01","statement":"x","rationale":"no"}]`,
		"bad id":           `[{"id":"D-0001","date":"2026-01-01","statement":"x"}]`,
		"id mismatch":      `[{"id":"pd-00000000","date":"2026-01-01","statement":"x"}]`,
		"empty statement":  `[{"id":"` + IDFor("") + `","date":"2026-01-01","statement":""}]`,
		"bad date":         `[{"id":"` + IDFor("x") + `","date":"yesterday","statement":"x"}]`,
		"trailing content": `[] []`,
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

func TestExtractDesignDecisionsSupersedesClause(t *testing.T) {
	body := "- **DD-1**: Old.\n  Decision: a.\n- **DD-2**: New.\n  Supersedes DD-1. Decision: b.\n- **DD-3**: Other.\n  Supersedes: Other:DD-4 because reasons.\n"
	got := ExtractDesignDecisions(body)
	if got[0].Supersedes != "" || got[1].Supersedes != "DD-1" || got[2].Supersedes != "Other:DD-4" {
		t.Fatalf("supersedes = %q %q %q", got[0].Supersedes, got[1].Supersedes, got[2].Supersedes)
	}
}
