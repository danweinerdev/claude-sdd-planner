package hook

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Keep hook input bounded. A normal post-rewrite stream is only 82 bytes per
// rewritten SHA-1 commit; 8 MiB still permits more than 100,000 rows while
// preventing an accidental or hostile pipe from consuming unbounded memory.
const maxPostRewriteBytes = 8 << 20

const maxPostRewriteRows = 100_000

var gitObjectName = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)

// PostRewriteCapture is the immutable raw map persisted for an explicit later
// `graph remap-revisions` invocation.
type PostRewriteCapture struct {
	Path string
	Rows int
}

// CapturePostRewrite validates Git's post-rewrite stream and stores it without
// collapsing or otherwise interpreting rows. In particular, many-to-one rows
// are retained; the explicit remap command owns the policy decision about
// whether a graph can accept them.
func CapturePostRewrite(cwd, rewriteKind string, input io.Reader) (PostRewriteCapture, error) {
	if rewriteKind != "rebase" && rewriteKind != "amend" {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: rewrite kind must be `rebase` or `amend`, got %q", rewriteKind)
	}
	raw, err := io.ReadAll(io.LimitReader(input, maxPostRewriteBytes+1))
	if err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: reading stdin: %w", err)
	}
	if len(raw) > maxPostRewriteBytes {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: stdin exceeds the %d-byte limit", maxPostRewriteBytes)
	}
	rows, err := validatePostRewrite(raw)
	if err != nil {
		return PostRewriteCapture{}, err
	}

	dir, err := gitPrivatePath(cwd, "sdd/rewrite-maps")
	if err != nil {
		return PostRewriteCapture{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: creating Git-private map directory %s: %w", dir, err)
	}

	var nonce [12]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: generating map identity: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s.map", rewriteKind, time.Now().UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(nonce[:]))
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: creating unique raw map %s: %w", path, err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(raw); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: writing raw map %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: syncing raw map %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: closing raw map %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		return PostRewriteCapture{}, fmt.Errorf("post-rewrite: making raw map immutable %s: %w", path, err)
	}
	ok = true
	return PostRewriteCapture{Path: path, Rows: rows}, nil
}

func validatePostRewrite(raw []byte) (int, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("post-rewrite: stdin contained no old-new rows")
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return 0, fmt.Errorf("post-rewrite: stdin contains a NUL byte")
	}
	s := bufio.NewScanner(bytes.NewReader(raw))
	s.Buffer(make([]byte, 4096), 1024)
	rows := 0
	for s.Scan() {
		rows++
		if rows > maxPostRewriteRows {
			return 0, fmt.Errorf("post-rewrite: stdin exceeds the %d-row limit", maxPostRewriteRows)
		}
		fields := strings.Fields(s.Text())
		if len(fields) != 2 || !gitObjectName.MatchString(fields[0]) || !gitObjectName.MatchString(fields[1]) {
			return 0, fmt.Errorf("post-rewrite: invalid row %d; expected exactly two full 40- or 64-hex object names", rows)
		}
	}
	if err := s.Err(); err != nil {
		return 0, fmt.Errorf("post-rewrite: validating stdin: %w", err)
	}
	if rows == 0 {
		return 0, fmt.Errorf("post-rewrite: stdin contained no old-new rows")
	}
	return rows, nil
}

func gitPrivatePath(cwd, name string) (string, error) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--git-path", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("post-rewrite: resolving Git-private path: %w: %s", err, detail)
		}
		return "", fmt.Errorf("post-rewrite: resolving Git-private path: %w", err)
	}
	p := filepath.FromSlash(strings.TrimSpace(string(out)))
	if p == "" {
		return "", fmt.Errorf("post-rewrite: git returned an empty private path")
	}
	if !filepath.IsAbs(p) {
		absCWD, absErr := filepath.Abs(cwd)
		if absErr != nil {
			return "", fmt.Errorf("post-rewrite: resolving working directory: %w", absErr)
		}
		p = filepath.Join(absCWD, p)
	}
	return filepath.Clean(p), nil
}
