package decisionview

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var ErrStoreConflict = errors.New("decisionview: stale local write")

type LocalSnapshot struct {
	Exists bool
	Bytes  []byte
	Digest string
}
type LocalStore struct {
	root       *os.Root
	repository string
	planning   string
	key        string
	tryLock    func(*os.File) error
	afterCheck func()
}

func OpenLocalStore(repository, planning, key string) (*LocalStore, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("decisionview: stable store identity required")
	}
	var err error
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return nil, err
	}
	planning, err = filepath.EvalSymlinks(planning)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(planning)
	if err != nil {
		return nil, err
	}
	return &LocalStore{root: r, repository: repository, planning: planning, key: key, tryLock: lockLocalFile}, nil
}
func (s *LocalStore) Close() error { return s.root.Close() }
func (s *LocalStore) Read(relative string) (LocalSnapshot, error) {
	if err := validateRelativeLocator(relative); err != nil {
		return LocalSnapshot{}, err
	}
	b, _, err := readCollectionFile(s.root, relative)
	if os.IsNotExist(err) {
		return LocalSnapshot{}, nil
	}
	if err != nil {
		return LocalSnapshot{}, err
	}
	return LocalSnapshot{true, b, collectionDigest(b)}, nil
}
func (s *LocalStore) WriteExpected(ctx context.Context, relative string, content []byte, expected string) error {
	if ctx == nil {
		return fmt.Errorf("decisionview: write context required")
	}
	if err := validateRelativeLocator(relative); err != nil {
		return err
	}
	if len(content) > maxCollectionFileBytes {
		return fmt.Errorf("decisionview: local content exceeds read/write limit")
	}
	parentPath := filepath.Join(s.planning, filepath.FromSlash(path.Dir(relative)))
	repoInfo, repoErr := os.Stat(s.repository)
	if repoErr != nil {
		return repoErr
	}
	parentInfo, parentErr := os.Stat(parentPath)
	if parentErr == nil && os.SameFile(parentInfo, repoInfo) {
		return fmt.Errorf("decisionview: new local ledger storage cannot pollute the represented repository root")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lockDir := fmt.Sprintf("Decisions/.fork-state/%x", sha256.Sum256([]byte(s.key)))
	if err := s.root.MkdirAll(filepath.FromSlash(lockDir), 0o755); err != nil {
		return err
	}
	lock, err := s.root.OpenFile(filepath.FromSlash(lockDir+"/writer.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err = s.tryLock(lock)
		if err == nil {
			break
		}
		if !localLockBusy(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer unlockLocalFile(lock)
	current, err := s.Read(relative)
	if err != nil {
		return err
	}
	if current.Digest != expected {
		return ErrStoreConflict
	}
	mode := os.FileMode(0o644)
	if info, err := s.root.Lstat(filepath.FromSlash(relative)); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("decisionview: a local write cannot replace a symlink")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.afterCheck != nil {
		s.afterCheck()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parentName := path.Dir(relative)
	if err := s.root.MkdirAll(filepath.FromSlash(parentName), 0o755); err != nil {
		return err
	}
	parent, err := s.root.OpenRoot(filepath.FromSlash(parentName))
	if err != nil {
		return err
	}
	defer parent.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	base := path.Base(relative)
	temp := fmt.Sprintf(".%s.sdd-%x", base, nonce)
	f, err := parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer parent.Remove(temp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// An ordinary external edit is detected at the last cooperative check.
	// No lock can promise universal isolation from a non-cooperating writer.
	latest, err := s.Read(relative)
	if err != nil {
		return err
	}
	if latest.Digest != expected {
		return ErrStoreConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishLocalFile(parent, temp, base, current.Exists); err != nil {
		after, readErr := s.Read(relative)
		outcome := "unknown"
		if readErr == nil {
			if after.Digest == collectionDigest(content) {
				outcome = "committed"
			} else if after.Digest == expected {
				outcome = "not-committed"
			}
		}
		return &StoreOutcomeError{Outcome: outcome, Cause: err}
	}
	return nil
}

type StoreOutcomeError struct {
	Outcome string
	Cause   error
}

func (e *StoreOutcomeError) Error() string {
	return fmt.Sprintf("decisionview: publication outcome %s: %v", e.Outcome, e.Cause)
}
func (e *StoreOutcomeError) Unwrap() error { return e.Cause }
