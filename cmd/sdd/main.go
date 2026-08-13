// Command sdd is the SDD toolchain. Spike scope: schema inspection, artifact
// inspection, and `apply` for the `spec` artifact type.
//
// Exit codes follow FR-03: 0 success, 1 refused mutation or authoritative
// findings, 2 malformed invocation or the operation could not run.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// idDeclRe matches an identifier declaration in a list item, capturing the
// strikethrough markers that denote retirement.
var idDeclRe = regexp.MustCompile(`^\s*[-*+]\s*(?:\[[ xX]\]\s*)?(~~)?\*\*([A-Z]+-\d+)\*\*(~~)?\s*:`)

func main() {
	root := newRootCmd()
	root.SetArgs(os.Args[1:])
	err := root.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdd: %v\n", err)
		// Cobra already printed the suggestion for an unknown command; for a
		// malformed invocation, point at the right help rather than dumping
		// the whole usage block.
		if usageHint(err) {
			fmt.Fprintln(os.Stderr, "    run `sdd help` or `sdd <command> --help`")
		}
	}
	os.Exit(exitCode(err))
}

type refusedError struct{ n int }

func (e *refusedError) Error() string { return fmt.Sprintf("refused: %d violation(s)", e.n) }

// parseFlags parses args with fs, permitting flags to appear before or after
// positional arguments — which stdlib `flag` does not do on its own: it stops
// at the first non-flag argument, so `sdd show path --json` would leave --json
// unparsed. It returns the positional arguments in order.
//
// Which flags consume a following argument is read from the FlagSet itself,
// via the same IsBoolFlag interface flag.Parse uses. That is the point of this
// function: the previous implementation took a hand-written map of value-flag
// names, restated at all sixteen call sites in both `-x` and `--x` spellings.
// A flag added to the FlagSet but forgotten in that map silently mis-parsed —
// its value was classified as a positional, so the command either rejected a
// valid invocation or bound the value to the wrong parameter, with nothing to
// catch the drift. Derived, the two cannot disagree.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	takesValue := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		b, ok := f.Value.(interface{ IsBoolFlag() bool })
		if !ok || !b.IsBoolFlag() {
			takesValue[f.Name] = true
		}
	})

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-":
			// conventional stdin marker; ignored
		case a == "--":
			// everything after is positional, by convention
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
			// `--name=value` carries its own value; only the separated
			// `--name value` form consumes the next argument.
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(a, "=") && takesValue[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, a)
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return append(positional, fs.Args()...), nil
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
