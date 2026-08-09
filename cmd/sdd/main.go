// Command sdd is the SDD toolchain. Spike scope: schema inspection, artifact
// inspection, and `apply` for the `spec` artifact type.
//
// Exit codes follow FR-03: 0 success, 1 refused mutation or authoritative
// findings, 2 malformed invocation or the operation could not run.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const version = "0.0.0-spike"

// idDeclRe matches an identifier declaration in a list item, capturing the
// strikethrough markers that denote retirement.
var idDeclRe = regexp.MustCompile(`^\s*[-*+]\s*(?:\[[ xX]\]\s*)?(~~)?\*\*([A-Z]+-\d+)\*\*(~~)?\s*:`)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "version":
		fmt.Printf("sdd %s\n", version)
	case "schema":
		err = cmdSchema(os.Args[2:])
	case "apply":
		err = cmdApply(os.Args[2:])
	case "show":
		err = cmdShow(os.Args[2:])
	case "list":
		err = cmdList(os.Args[2:])
	case "section":
		err = cmdSection(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "sdd: unknown subcommand %q\n", os.Args[1])
		if s := nearest(os.Args[1], subcommands); s != "" {
			fmt.Fprintf(os.Stderr, "    did you mean %q?\n", s)
		}
		usage()
		os.Exit(2)
	}
	if err != nil {
		var re *refusedError
		if errors.As(err, &re) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "sdd: %v\n", err)
		os.Exit(2)
	}
}

var subcommands = []string{"version", "schema", "apply", "show", "list", "section", "doctor", "migrate", "help"}

func usage() {
	fmt.Fprint(os.Stderr, `sdd — SDD toolchain (spike)

  sdd version
  sdd schema list
  sdd schema show <type>
  sdd show <artifact-path> [--json]
  sdd list [spec|design|plan|research] [--root PATH] [--json]
  sdd apply <artifact-path> [--dry-run] [--diff] [--json]
                            [--expect DIGEST] [--retire ID[,ID...]] [--create]
  sdd section set <artifact-path> --heading "## Overview" [--dry-run] [--diff]
                                  [--json] [--expect DIGEST] [--type T]
  sdd doctor [--json]
  sdd migrate <artifact-path> [--dry-run] [--diff] [--json] [--allow-frozen]

apply reads a Markdown proposal on stdin. Without --dry-run it writes the
compiled artifact atomically. Pass --expect with the digest you read to refuse
the write if the artifact changed underneath you.

section set reads the new section body on stdin and replaces only that
section, leaving every other section and the frontmatter (aside from
`+"`updated`"+`) byte-identical.

migrate is the upgrade path for artifacts that predate the schema: it inserts
missing required sections and author frontmatter from schema defaults and reports
every insertion. It is a separate verb on purpose — apply keeps refusing
non-compliant structure, which is the reason writes go through a compiler at all.

doctor reports the binary's identity, the resolved planning root, and the
embedded schema set with per-type artifact counts; it exits 2 if the planning
root cannot be resolved.
`)
}

type refusedError struct{ n int }

func (e *refusedError) Error() string { return fmt.Sprintf("refused: %d violation(s)", e.n) }

// splitArgs separates flags from positional arguments so that flags may appear
// before or after the path, as every comparable CLI allows. valueFlags names the
// flags that consume a following argument.
func splitArgs(args []string, valueFlags map[string]bool) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-":
			// conventional stdin marker; ignored
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
			if valueFlags[a] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, a)
		}
	}
	return
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// relPath renders a path relative to the working directory with forward slashes
// (FR-10), falling back to the input when it lies outside.
func relPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.ToSlash(p)
	}
	r, err := filepath.Rel(wd, abs)
	if err != nil || strings.HasPrefix(r, "..") {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// nearest returns the closest candidate within a small edit distance, so an
// unknown subcommand or flag gets a suggestion rather than bare usage (FR-29).
func nearest(got string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := editDistance(got, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// lineDiff is a compact longest-common-subsequence diff, sufficient for
// eyeballing what normalization would change.
func lineDiff(a, b string) string {
	if a == b {
		return ""
	}
	al := strings.Split(strings.TrimRight(a, "\n"), "\n")
	bl := strings.Split(strings.TrimRight(b, "\n"), "\n")
	if a == "" {
		al = nil
	}

	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			fmt.Fprintf(&out, "  -%d| %s\n", i+1, al[i])
			i++
		default:
			fmt.Fprintf(&out, "  +%d| %s\n", j+1, bl[j])
			j++
		}
	}
	for ; i < n; i++ {
		fmt.Fprintf(&out, "  -%d| %s\n", i+1, al[i])
	}
	for ; j < m; j++ {
		fmt.Fprintf(&out, "  +%d| %s\n", j+1, bl[j])
	}
	return out.String()
}
