package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	stagePrefix  = ".skiller-stage-"
	backupPrefix = ".skiller-backup-"
)

type Options struct {
	Suffix  func() string
	Rename  func(oldpath, newpath string) error
	Symlink func(oldname, newname string) error
}

type Result struct {
	TargetPath      string
	Requested       string
	Effective       string
	FallbackApplied bool
	BackupPath      string
	Writes          []string
}

func CopyDir(source, target string, mutate func(stage string) error, opts Options) (Result, error) {
	result := Result{TargetPath: target, Requested: "copy", Effective: "copy"}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return result, err
	}
	stage := siblingPath(target, stagePrefix, opts)
	backup := siblingPath(target, backupPrefix, opts)
	if err := os.RemoveAll(stage); err != nil {
		return result, err
	}
	if err := copyTree(source, stage); err != nil {
		_ = os.RemoveAll(stage)
		return result, err
	}
	if mutate != nil {
		if err := mutate(stage); err != nil {
			_ = os.RemoveAll(stage)
			return result, err
		}
	}
	if err := promote(stage, target, backup, opts.rename()); err != nil {
		_ = os.RemoveAll(stage)
		return result, err
	}
	result.BackupPath = backup
	result.Writes = []string{target}
	return result, nil
}

func CopyFile(source, target string, mutate func(stage string) error, opts Options) (Result, error) {
	result := Result{TargetPath: target, Requested: "copy", Effective: "copy"}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return result, err
	}
	stage := siblingPath(target, stagePrefix, opts)
	backup := siblingPath(target, backupPrefix, opts)
	if err := os.RemoveAll(stage); err != nil {
		return result, err
	}
	if err := copyFile(source, stage); err != nil {
		_ = os.RemoveAll(stage)
		return result, err
	}
	if mutate != nil {
		if err := mutate(stage); err != nil {
			_ = os.RemoveAll(stage)
			return result, err
		}
	}
	if err := promote(stage, target, backup, opts.rename()); err != nil {
		_ = os.RemoveAll(stage)
		return result, err
	}
	result.BackupPath = backup
	result.Writes = []string{target}
	return result, nil
}

func WriteFile(target string, data []byte, perm fs.FileMode, opts Options) (Result, error) {
	result := Result{TargetPath: target, Requested: "copy", Effective: "copy"}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return result, err
	}
	stage := siblingPath(target, stagePrefix, opts)
	backup := siblingPath(target, backupPrefix, opts)
	if err := os.WriteFile(stage, data, perm); err != nil {
		_ = os.Remove(stage)
		return result, err
	}
	bestEffortSyncFile(stage)
	if err := promote(stage, target, backup, opts.rename()); err != nil {
		_ = os.Remove(stage)
		return result, err
	}
	result.BackupPath = backup
	result.Writes = []string{target}
	return result, nil
}

func LinkOrCopyDir(source, target string, mutateCopy func(stage string) error, opts Options) (Result, error) {
	result := Result{TargetPath: target, Requested: "link", Effective: "link"}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return result, err
	}
	tempLink := siblingPath(target, ".skiller-link-", opts)
	_ = os.Remove(tempLink)
	if err := opts.symlink()(source, tempLink); err != nil {
		copyResult, copyErr := CopyDir(source, target, mutateCopy, opts)
		copyResult.Requested = "link"
		copyResult.Effective = "copy"
		copyResult.FallbackApplied = true
		return copyResult, copyErr
	}
	backup := siblingPath(target, backupPrefix, opts)
	if err := promote(tempLink, target, backup, opts.rename()); err != nil {
		_ = os.Remove(tempLink)
		return result, err
	}
	result.BackupPath = backup
	result.Writes = []string{target}
	return result, nil
}

func promote(stage, target, backup string, rename func(string, string) error) error {
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := rename(stage, target); err != nil {
				return renameErr(stage, target, err)
			}
			bestEffortSyncDir(filepath.Dir(target))
			return nil
		}
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := rename(target, backup); err != nil {
		return renameErr(target, backup, err)
	}
	if err := rename(stage, target); err != nil {
		_ = rename(backup, target)
		return renameErr(stage, target, err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	bestEffortSyncDir(filepath.Dir(target))
	return nil
}

func SweepOrphans(parent string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !isOrphanName(name) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, name)); err != nil {
			return err
		}
	}
	return nil
}

func isOrphanName(name string) bool {
	return len(name) > len(stagePrefix) && name[:len(stagePrefix)] == stagePrefix ||
		len(name) > len(backupPrefix) && name[:len(backupPrefix)] == backupPrefix
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, dst)
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		return copyFile(path, dst)
	})
}

func copyFile(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	bestEffortSyncFile(target)
	return nil
}

func siblingPath(target, prefix string, opts Options) string {
	return filepath.Join(filepath.Dir(target), prefix+opts.suffix())
}

func renameErr(oldpath, newpath string, err error) error {
	if errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("rename %s -> %s crossed filesystems: %w", oldpath, newpath, err)
	}
	return fmt.Errorf("rename %s -> %s: %w", oldpath, newpath, err)
}

func (opts Options) suffix() string {
	if opts.Suffix != nil {
		return opts.Suffix()
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(buf[:])
}

func (opts Options) rename() func(string, string) error {
	if opts.Rename != nil {
		return opts.Rename
	}
	return os.Rename
}

func (opts Options) symlink() func(string, string) error {
	if opts.Symlink != nil {
		return opts.Symlink
	}
	return os.Symlink
}

func bestEffortSyncFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	_ = file.Sync()
	_ = file.Close()
}

func bestEffortSyncDir(path string) {
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	_ = dir.Sync()
	_ = dir.Close()
}
