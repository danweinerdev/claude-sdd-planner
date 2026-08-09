package compile

import (
	"strings"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/internal/artifact"
	"github.com/danweinerdev/claude-sdd-planner/internal/schema"
)

// A duplicate top-level key is refused in every mode, including the upgrade
// path. Consumers disagree about which value wins — PyYAML keeps the last, a
// line-oriented reader the first — so an artifact with two `status:` keys reads
// as one state to one tool and another state to another. Picking for the author
// would silently choose a lifecycle state.
func TestDuplicateFrontmatterKeyAlwaysRefuses(t *testing.T) {
	p := strings.Replace(payload(nil), "tags: [x]", "tags: [x]\ntags: [y]", 1)

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"create", Options{Today: "2026-08-08"}},
		{"upgrade", Options{Today: "2026-08-08", Upgrade: true, StubSections: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(load(t), p, tc.opts)
			if r.OK() {
				t.Fatal("compile succeeded with a duplicate frontmatter key")
			}
			if !hasCode(r, "SPK023") {
				t.Errorf("codes = %v, want SPK023", codes(r))
			}
			var ref Refusal
			for _, x := range r.Refusals {
				if x.Code == "SPK023" {
					ref = x
				}
			}
			if !strings.Contains(ref.Message, "tags") {
				t.Errorf("refusal does not name the key: %s", ref.Message)
			}
			if !strings.Contains(ref.Message, "line") {
				t.Errorf("refusal does not name the other line: %s", ref.Message)
			}
			if !strings.Contains(ref.Correction, "cannot be resolved mechanically") {
				t.Errorf("correction should say why it is not auto-fixed: %s", ref.Correction)
			}
		})
	}
}

func TestDuplicateDetectionAcceptsDistinctKeys(t *testing.T) {
	r := compileNew(t, payload(nil))
	mustOK(t, r)
}

// A legacy spelling whose meaning is identical is renamed by the upgrade path
// and reported. Only such spellings may be aliased: a rename is mechanical, so
// an "alias" whose semantics differ cannot be handled this way.
func TestAliasRenamedOnUpgrade(t *testing.T) {
	s, err := schema.Load("design")
	if err != nil {
		t.Fatal(err)
	}
	f := s.Field("implemented_in")
	if f == nil {
		t.Fatal("design schema does not declare implemented_in")
	}
	found := false
	for _, a := range f.Aliases {
		if a == "implemented_by" {
			found = true
		}
	}
	if !found {
		t.Fatalf("implemented_in aliases = %v, want implemented_by", f.Aliases)
	}
	if s.FieldByAlias("implemented_by") == nil {
		t.Error("FieldByAlias(implemented_by) = nil")
	}
	// A key that is not an alias must not resolve to one.
	if s.FieldByAlias("supersedes") != nil {
		t.Error("supersedes resolved as an alias; its direction is opposite to superseded_by and it must be reported, not renamed")
	}
}

// supersedes and superseded_by are opposite directions of one relationship, so
// they are declared as a distinct PAIR rather than aliased together: `A
// supersedes B` and `A superseded_by B` make contradictory claims, and renaming
// one to the other would invert the link. Declaring both is also what makes a
// supersession chain traversable in either direction, matching the ledger
// convention in shared/decision-log.md.
func TestSupersessionPairDeclaredSeparately(t *testing.T) {
	for _, ty := range []string{"spec", "design"} {
		s, err := schema.Load(ty)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"supersedes", "superseded_by"} {
			f := s.Field(key)
			if f == nil {
				t.Errorf("%s schema does not declare %s", ty, key)
				continue
			}
			if f.Required {
				t.Errorf("%s.%s is required; supersession is optional", ty, key)
			}
			if len(f.Aliases) != 0 {
				t.Errorf("%s.%s declares aliases %v; the two directions must never alias to each other", ty, key, f.Aliases)
			}
		}
	}
}

// Ordinary apply refuses an unmodeled key outright: frontmatter is
// tool-controlled and there is deliberately no extras mechanism.
func TestUndeclaredKeyRefusedOnApply(t *testing.T) {
	p := strings.Replace(payload(nil), "tags: [x]", "commit: abc123\ntags: [x]", 1)
	r := compileNew(t, p)
	if r.OK() {
		t.Fatal("compile succeeded with an unmodeled frontmatter key")
	}
	if !hasCode(r, "SPK020") {
		t.Errorf("codes = %v, want SPK020", codes(r))
	}
}

// The upgrade path neither drops the value nor accepts it silently: it carries
// it through so migration is lossless, and reports it as work to do.
func TestUndeclaredKeyCarriedAndReportedOnUpgrade(t *testing.T) {
	base := compileNew(t, payload(nil))
	mustOK(t, base)
	src := strings.Replace(base.Output, "tags: [x]", "commit: abc123\ntags: [x]", 1)

	r := Compile(load(t), src, Options{
		Today: "2026-08-08", Existing: artifact.Parse(src),
		Upgrade: true, StubSections: true,
	})
	mustOK(t, r)
	if !strings.Contains(r.Output, "commit: abc123") {
		t.Errorf("upgrade dropped the unmodeled key; migration must be lossless:\n%s", r.Output)
	}
	joined := strings.Join(r.Todos, "; ")
	if !strings.Contains(joined, "commit") {
		t.Errorf("todos = %v, want one naming commit", r.Todos)
	}
	if !strings.Contains(joined, "will refuse on the next apply") {
		t.Errorf("todo should say the artifact is not yet compliant: %v", r.Todos)
	}
}
