// Package intent computes the requirement fingerprints behind INTENT-STALE
// (Designs/SddGraph DD-4): compile embeds the SHA-256 of each cited
// requirement's normalized text into the citing node, and reads recompute it
// — a mismatch means the spec moved under the node.
//
// The hash input is defined, not incidental (DD-4 "Hash input, defined"):
// the span from an identifier's definition token to the next definition or
// heading, normalized formatting-insensitively — whitespace runs collapsed,
// line wraps joined, list markers, checkboxes, and emphasis stripped — so a
// rewrapped or re-marked requirement does not fire, while any wording or
// literal-value change does. Judging whether a reword is synonymous is the
// LLM's job at resolution time, never the hasher's.
//
// One normalizer, one hasher, used by embed and recheck alike: two
// implementations would eventually disagree, and a fingerprint nobody can
// reproduce is noise. Identifier discovery reuses the validator's own
// definition patterns (rules.DefinitionPattern), so an item this package
// fingerprints is exactly an item the validator says exists.
package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/rules"
)

// Item is one identifier definition and its fingerprint.
type Item struct {
	ID         string
	Family     string
	Normalized string
	Hash       string
}

// headingRe bounds spans: a heading of any depth ends the item before it.
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s`)

// Items extracts every FR/NFR/AC/DD definition from a comment-stripped body
// (pass the artifact body through rules.CommentStripped first, the same
// preprocessing the validator's scans apply). The map is keyed by id; a
// duplicate definition keeps the first occurrence, mirroring the
// validator's first-wins posture for duplicates it flags elsewhere.
func Items(body string) map[string]Item {
	type boundary struct {
		offset int
		id     string
		family string
	}
	var defs []boundary
	for _, family := range rules.IdentifierFamilies() {
		re := rules.DefinitionPattern(family)
		if re == nil {
			continue
		}
		for _, m := range re.FindAllStringSubmatchIndex(body, -1) {
			defs = append(defs, boundary{offset: m[0], id: body[m[2]:m[3]], family: family})
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].offset < defs[j].offset })

	// Every definition start and every heading start bounds the item before
	// it.
	bounds := make([]int, 0, len(defs))
	for _, d := range defs {
		bounds = append(bounds, d.offset)
	}
	for _, m := range headingRe.FindAllStringIndex(body, -1) {
		bounds = append(bounds, m[0])
	}
	sort.Ints(bounds)

	out := make(map[string]Item, len(defs))
	for _, d := range defs {
		end := len(body)
		for _, b := range bounds {
			if b > d.offset {
				end = b
				break
			}
		}
		if _, dup := out[d.id]; dup {
			continue
		}
		normalized := Normalize(body[d.offset:end])
		out[d.id] = Item{ID: d.id, Family: d.family, Normalized: normalized, Hash: Hash(normalized)}
	}
	return out
}

// markerRe strips a line's leading list machinery: indentation, a bullet
// (-/*/+), and an optional checkbox.
var markerRe = regexp.MustCompile(`^\s*(?:[-*+]\s+)?(?:\[[ xX]\]\s+)?`)

// emphasisRe strips inline formatting characters: bold/italic/strikethrough
// markers and code-span backticks. Contents survive — a literal value inside
// backticks still counts (DD-4) — only the markup does not.
var emphasisRe = regexp.MustCompile("[*_`~]+")

// spaceRe collapses whitespace runs, which also joins re-wrapped lines.
var spaceRe = regexp.MustCompile(`\s+`)

// Normalize applies the defined hash-input normalization to one item span.
func Normalize(span string) string {
	lines := strings.Split(span, "\n")
	for i, line := range lines {
		lines[i] = markerRe.ReplaceAllString(line, "")
	}
	joined := strings.Join(lines, " ")
	joined = emphasisRe.ReplaceAllString(joined, "")
	joined = spaceRe.ReplaceAllString(joined, " ")
	return strings.TrimSpace(joined)
}

// Hash fingerprints a normalized span. The `sha256:` prefix travels with the
// value so a future algorithm change is a visible migration, not a silent
// mismatch.
func Hash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}
