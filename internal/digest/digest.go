package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mostlydev/skiller/internal/hashid"
)

func Path(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if !info.IsDir() {
		if err := hashFile(hash, path, filepath.Base(path)); err != nil {
			return "", err
		}
		return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if IsInstallerMetadata(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		return hashFile(hash, p, filepath.ToSlash(rel))
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func IsInstallerMetadata(name string) bool {
	switch name {
	case ".skiller-install.json", ".our-managed.json", ".gnit-skill-managed":
		return true
	default:
		return false
	}
}

func Short(value string) string {
	return hashid.Short(value)
}

func hashFile(hash io.Writer, path, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(hash, "file\x00"+name+"\x00"); err != nil {
		return err
	}
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	_, err = io.WriteString(hash, "\x00")
	return err
}
