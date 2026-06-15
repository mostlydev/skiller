package source

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/digest"
	"github.com/mostlydev/skiller/pkg/manifest"
)

type Snapshot = contract.SourceSnapshot

type Options struct {
	ManifestDir string
	Home        string
}

func ResolveAll(m manifest.Manifest, opts Options) ([]Snapshot, map[string]Snapshot, error) {
	resolver := resolver{opts: opts, byKey: map[string]Snapshot{}}
	for _, skill := range m.Skills {
		if _, err := resolver.resolveLocal(skill.Source); err != nil {
			return nil, nil, err
		}
	}
	for _, extra := range m.Extras {
		if _, err := resolver.resolveLocal(extra.Source); err != nil {
			return nil, nil, err
		}
	}
	bySpec := map[string]Snapshot{}
	for _, source := range resolver.sources {
		bySpec[source.OriginalSpec] = source
	}
	return resolver.sources, bySpec, nil
}

type resolver struct {
	opts    Options
	sources []Snapshot
	byKey   map[string]Snapshot
}

func (r *resolver) resolveLocal(spec string) (Snapshot, error) {
	sourcePath := resolvePath(spec, r.opts.ManifestDir, r.opts.Home)
	realpath, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("source %s does not exist", sourcePath)
		}
		realpath = sourcePath
	}
	sourceDigest, err := digest.Path(realpath)
	if err != nil {
		return Snapshot{}, err
	}
	key := "file:" + digest.Short(realpath)
	if existing, ok := r.byKey[key]; ok {
		return existing, nil
	}
	name, description := parseSkillFrontmatter(filepath.Join(realpath, "SKILL.md"))
	snapshot := Snapshot{
		ID:              fmt.Sprintf("source-%03d", len(r.sources)+1),
		SourceKind:      "file",
		OriginalSpec:    spec,
		CanonicalURI:    fileURI(realpath),
		SourceKey:       key,
		SourceStatus:    "refreshed",
		LocalCachePath:  realpath,
		SourceRealpath:  realpath,
		SourceDigest:    sourceDigest,
		FrontmatterName: name,
		Description:     description,
	}
	r.byKey[key] = snapshot
	r.sources = append(r.sources, snapshot)
	return snapshot, nil
}

func resolvePath(path, base, home string) string {
	path = expandPath(path, home)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(path)))
}

func expandPath(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	if path == "~" {
		return home
	}
	return filepath.Clean(os.ExpandEnv(filepath.FromSlash(path)))
}

func fileURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

func parseSkillFrontmatter(path string) (string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return "", ""
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", ""
	}
	frontmatter := text[4 : 4+end]
	var name, description string
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description
}
