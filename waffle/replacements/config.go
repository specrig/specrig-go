package replacements

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/specrig/specrig-go/waffle/internal/gomod"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Allow map[string][]string `yaml:"allow"`
}

func (c Config) Allowed(path, name string) bool {
	return slices.Contains(c.Allow[path], name)
}

// A typo would otherwise read as a denial: the check names a seam the list looks
// like it already grants.
func (c Config) Validate() error {
	for path, names := range c.Allow {
		if strings.TrimSpace(path) == "" {
			return errors.New("specrig.yaml: replacements.allow has an entry with an empty import path")
		}
		if len(names) == 0 {
			return fmt.Errorf("specrig.yaml: replacements.allow lists %s with no interfaces", path)
		}
		for _, name := range names {
			if !token.IsIdentifier(name) || !token.IsExported(name) {
				return fmt.Errorf("specrig.yaml: replacements.allow lists %q under %s, which is not an exported Go identifier",
					name, path)
			}
		}
	}
	return nil
}

type projectFile struct {
	Replacements Config `yaml:"replacements"`
}

// Absent grants nothing: no file, or no key, is an answer, and the answer is no.
func LoadConfig(root string) (Config, error) {
	fh, err := os.Open(filepath.Join(root, "specrig.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("failed to open specrig.yaml: %w", err)
	}
	//nolint:errcheck
	defer fh.Close()

	// Not KnownFields: this reader is interested in one group, and the rest of
	// the file belongs to generators it must not have an opinion about.
	var file projectFile
	if err := yaml.NewDecoder(fh).Decode(&file); err != nil {
		return Config{}, fmt.Errorf("failed to parse specrig.yaml: %w", err)
	}
	if err := file.Replacements.Validate(); err != nil {
		return Config{}, err
	}
	return file.Replacements, nil
}

func Module(root string) (string, error) { return gomod.Path(root) }

func ModuleRoot() (string, error) { return gomod.Root() }
