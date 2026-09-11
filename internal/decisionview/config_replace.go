package decisionview

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
)

type configReplacementFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type configReplacementOps struct {
	create func(*os.Root, string) (configReplacementFile, string, error)
	remove func(*os.Root, string) error
	rename func(*os.Root, string, string) error
	syncDir func(*os.Root) error
}

func replaceConfigFile(root *os.Root, target string, content []byte, ops configReplacementOps) error {
	if ops.create == nil || ops.remove == nil || ops.rename == nil {
		return errors.New("decisionview: complete config replacement operations required")
	}
	staged, stagedPath, err := ops.create(root, target)
	if err != nil {
		return err
	}
	cleanStaged := func() {
		_ = staged.Close()
		_ = ops.remove(root, stagedPath)
	}
	written, err := staged.Write(content)
	if err != nil {
		cleanStaged()
		return err
	}
	if written != len(content) {
		cleanStaged()
		return io.ErrShortWrite
	}
	if err := staged.Sync(); err != nil {
		cleanStaged()
		return err
	}
	if err := staged.Close(); err != nil {
		_ = ops.remove(root, stagedPath)
		return err
	}
	if err := ops.remove(root, target); err != nil {
		return fmt.Errorf("decisionview: remove old config; staged bytes retained at %q: %w", stagedPath, err)
	}
	if err := ops.rename(root, stagedPath, target); err != nil {
		return fmt.Errorf("decisionview: rename staged config; staged bytes retained at %q: %w", stagedPath, err)
	}
	if ops.syncDir != nil {
		if err := ops.syncDir(root); err != nil {
			return fmt.Errorf("decisionview: config replacement installed but directory sync failed: %w", err)
		}
	}
	return nil
}

func publishConfigTransactionFile(root *os.Root, relative string, content []byte) error {
	if len(content) > maxCollectionFileBytes {
		return errors.New("decisionview: transaction content exceeds file limit")
	}
	parent, err := root.OpenRoot(path.Dir(relative))
	if err != nil {
		return err
	}
	defer parent.Close()
	base := path.Base(relative)
	info, err := parent.Lstat(base)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("decisionview: transaction target is not a regular file")
	}
	return replaceConfigFile(parent, base, content, defaultConfigReplacementOps(info.Mode().Perm()))
}

func defaultConfigReplacementOps(mode os.FileMode) configReplacementOps {
	return configReplacementOps{
		create: func(root *os.Root, target string) (configReplacementFile, string, error) {
			var nonce [16]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return nil, "", err
			}
			stagedPath := fmt.Sprintf(".%s.config-%x", target, nonce)
			file, err := root.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				return nil, "", err
			}
			if err := file.Chmod(mode); err != nil {
				_ = file.Close()
				_ = root.Remove(stagedPath)
				return nil, "", err
			}
			return file, stagedPath, nil
		},
		remove: func(root *os.Root, name string) error {
			return root.Remove(name)
		},
		rename: func(root *os.Root, old, new string) error {
			return root.Rename(old, new)
		},
		syncDir: func(root *os.Root) error {
			if runtime.GOOS == "windows" {
				return nil
			}
			dir, err := root.Open(".")
			if err != nil {
				return err
			}
			defer dir.Close()
			return dir.Sync()
		},
	}
}
