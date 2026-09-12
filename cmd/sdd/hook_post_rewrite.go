package main

import (
	"fmt"
	"io"

	"github.com/danweinerdev/claude-sdd-planner/v2/internal/hook"
)

// cmdHookPostRewrite captures Git's native old-new stream only. Applying that
// identity to a plan graph remains an explicit, digest-guarded user action.
func cmdHookPostRewrite(cwd, rewriteKind string, in io.Reader, out io.Writer) error {
	capture, err := hook.CapturePostRewrite(cwd, rewriteKind, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "captured %d Git %s rewrite row(s) at %s\n", capture.Rows, rewriteKind, capture.Path)
	fmt.Fprintln(out, "capture only: no graph or completion evidence was changed")
	fmt.Fprintf(out, "next, preview the explicit remap: sdd graph remap-revisions --plan <plan> --map %q --dry-run\n", capture.Path)
	return nil
}
