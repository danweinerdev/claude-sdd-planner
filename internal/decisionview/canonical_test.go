package decisionview_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/decisionview"
)

func TestForkCanonicalHostileRoundTrip(t *testing.T) {
	entry := map[string]any{"text": "yes\n\"no\" 雪", "flags": []any{true, false, nil}}
	want := []byte("o2:s5:flagsa3:tfns4:texts12:yes\n\"no\" 雪")
	got, err := decisionview.EncodeEntry("entry-v1", entry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("typed golden mismatch: %q != %q", got, want)
	}
	decoded, err := decisionview.DecodeEntry("entry-v1", got)
	if err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != entry["text"] {
		t.Fatal("hostile text lost")
	}
	again, err := decisionview.EncodeEntry("entry-v1", decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, want) {
		t.Fatal("reparsed bytes changed")
	}
	for _, bad := range []string{"o1:s1:ai01;", "o2:s1:bns1:an", "o2:s1:ans1:an", "o1:s5:short", "o999999999999999999999999999:", "o1:s1:xz", "o0:extra"} {
		if _, err := decisionview.DecodeEntry("entry-v1", []byte(bad)); err == nil {
			t.Errorf("accepted invalid encoding %q", bad)
		}
	}
}

func TestForkCanonicalSeedReplay(t *testing.T) {
	replay := func(seed int64, reverse bool) []byte {
		r := rand.New(rand.NewSource(seed))
		var trace bytes.Buffer
		for i := 0; i < 40; i++ {
			values := []any{fmt.Sprintf("D-%04d", i+1), json.Number(fmt.Sprint(r.Intn(10000))), "\"reserved\"\ntrue"}
			keys := []string{"id", "value", "statement"}
			entry := map[string]any{}
			for j := range keys {
				k := j
				if reverse {
					k = len(keys) - 1 - j
				}
				entry[keys[k]] = values[k]
			}
			encoded, err := decisionview.EncodeEntry("entry-v1", entry)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := decisionview.DigestEntry("entry-v1", entry)
			if err != nil {
				t.Fatal(err)
			}
			independent := fmt.Sprintf("sha256:%x", sha256.Sum256(append([]byte("sdd-decision-entry-v1\x00"), encoded...)))
			if digest != independent {
				t.Fatal("digest lacks the specified version domain")
			}
			trace.Write(encoded)
			trace.WriteString(digest)
		}
		return trace.Bytes()
	}
	a, b := replay(42, false), replay(42, true)
	if !bytes.Equal(a, b) {
		t.Fatal("same seed produced different byte traces across independent construction orders")
	}
	if bytes.Equal(a, replay(43, false)) {
		t.Fatal("different source values were ignored")
	}
}

func TestForkCanonicalFieldMatrix(t *testing.T) {
	base := map[string]any{"id": "D-0001", "statement": "rule", "scope": []any{}, "rationale": "reason", "rejected": []any{}, "confirmation": "check", "reversibility": "two-way", "status": "accepted", "date": "2026-09-08", "decided_by": "user", "kind": "decision"}
	original, err := decisionview.DigestEntry("entry-v1", base)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"statement", "scope", "rationale", "rejected", "confirmation", "reversibility", "status", "supersedes", "superseded_by", "question", "refresh_when", "extension"} {
		changed := map[string]any{}
		for k, v := range base {
			changed[k] = v
		}
		changed[key] = "changed"
		got, err := decisionview.DigestEntry("entry-v1", changed)
		if err != nil {
			t.Fatal(err)
		}
		if got == original {
			t.Errorf("decision-bearing field %s ignored", key)
		}
	}
	for _, v := range []any{1.5, json.Number("1e0"), map[int]string{1: "no"}, []byte{1, 2}, make(chan int)} {
		if _, err := decisionview.EncodeEntry("entry-v1", map[string]any{"value": v}); err == nil {
			t.Errorf("accepted unsupported type %T", v)
		}
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err := decisionview.EncodeEntry("entry-v1", cycle); err == nil {
		t.Fatal("cyclic input accepted")
	}
	if _, err := decisionview.EncodeEntry("entry-v2", base); err == nil {
		t.Fatal("unknown encoding version accepted")
	}
	if _, err := decisionview.DecodeEntry("entry-v2", []byte("o0:")); err == nil {
		t.Fatal("unknown decode version accepted")
	}
	empty, err := decisionview.EncodeEntry("entry-v1", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	null, err := decisionview.EncodeEntry("entry-v1", map[string]any{"value": nil})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(empty, null) {
		t.Fatal("absence and null collapsed")
	}
	one, err := decisionview.EncodeEntry("entry-v1", map[string]any{"n": int64(1)})
	if err != nil {
		t.Fatal(err)
	}
	two, err := decisionview.EncodeEntry("entry-v1", map[string]any{"n": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatal("equivalent parsed integers encoded differently")
	}
}
