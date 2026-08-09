package rules

// Family (a): the artifact-discovery/parse gate, ported from Validator._parse.
// Every code here fires at most once per file and, except SDD003, stops that
// file from being modeled further — mirrored by ParseStage in root.go.

func init() {
	Register(&Rule{
		Code: "SDD002", Severity: Error, PyFunc: "_parse",
		What: "an artifact file is not readable as UTF-8",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.ParseStage != "SDD002" {
				return
			}
			emit(Diagnostic{
				Code: "SDD002", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "Cannot read UTF-8 artifact: " + a.ParseDetail,
				Correction: "Store the artifact as readable UTF-8.",
			})
		},
		Bad: []Example{{Name: "invalid-utf8", Files: map[string]string{
			"Research/bad.md": "---\ntitle: x\n---\n\xff\xfe",
		}}},
		Good: []Example{{Name: "valid-utf8", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD003", Severity: Error, PyFunc: "_parse",
		What: "an artifact uses CRLF line endings",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if !a.HasCRLF {
				return
			}
			emit(Diagnostic{
				Code: "SDD003", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "Artifact uses CRLF line endings.",
				Correction: "Normalize it to UTF-8 with LF endings.",
			})
		},
		Bad: []Example{{Name: "crlf", Files: map[string]string{
			"Research/bad.md": strCRLF(validResearch),
		}}},
		Good: []Example{{Name: "lf", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD004", Severity: Error, PyFunc: "_parse",
		What: "an artifact is missing the opening `---` frontmatter delimiter",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.ParseStage != "SDD004" {
				return
			}
			emit(Diagnostic{
				Code: "SDD004", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "Missing opening YAML frontmatter delimiter.",
				Correction: "Start the artifact with `---` YAML frontmatter.",
			})
		},
		Bad: []Example{{Name: "no-frontmatter", Files: map[string]string{
			"Research/bad.md": "# Just a heading\n\nbody text\n",
		}}},
		Good: []Example{{Name: "has-frontmatter", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD005", Severity: Error, PyFunc: "_parse",
		What: "an artifact is missing the closing `---` frontmatter delimiter",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.ParseStage != "SDD005" {
				return
			}
			emit(Diagnostic{
				Code: "SDD005", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "Missing closing YAML frontmatter delimiter.",
				Correction: "Close frontmatter with a standalone `---`.",
			})
		},
		Bad: []Example{{Name: "unclosed", Files: map[string]string{
			"Research/bad.md": "---\ntitle: x\ntype: research\n",
		}}},
		Good: []Example{{Name: "closed", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD006", Severity: Error, PyFunc: "_parse",
		What: "an artifact's frontmatter is not valid YAML",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.ParseStage != "SDD006" {
				return
			}
			line := a.ParseLine
			if line == 0 {
				line = 1
			}
			emit(Diagnostic{
				Code: "SDD006", Severity: Error, Path: a.Rel, Line: line,
				Message:    "Invalid YAML frontmatter: " + a.ParseDetail,
				Correction: "Correct the YAML syntax.",
			})
		},
		Bad: []Example{{Name: "unparsable-line", Files: map[string]string{
			"Research/bad.md": "---\ntitle x\ntype: research\n---\nbody\n",
		}}},
		Good: []Example{{Name: "valid", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})

	Register(&Rule{
		Code: "SDD007", Severity: Error, PyFunc: "_parse",
		What: "an artifact's frontmatter is not a mapping",
		Check: func(a *Artifact, emit func(Diagnostic)) {
			if a.ParseStage != "SDD007" {
				return
			}
			emit(Diagnostic{
				Code: "SDD007", Severity: Error, Path: a.Rel, Line: 1,
				Message:    "Frontmatter is not a mapping.",
				Correction: "Use key/value YAML frontmatter.",
			})
		},
		Bad: []Example{{Name: "list-frontmatter", Files: map[string]string{
			"Research/bad.md": "---\n- a\n- b\n---\nbody\n",
		}}},
		Good: []Example{{Name: "mapping", Files: map[string]string{
			"Research/ok.md": validResearch,
		}}},
	})
}

func strCRLF(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, '\r', '\n')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
