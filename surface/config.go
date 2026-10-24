package surface

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile holds what a record cannot learn from the code, at the service's root.
const ConfigFile = "specrig.yaml"

type Config struct {
	Clients  map[string]string `yaml:"clients"`
	Prefixes []string          `yaml:"prefixes"`
}

func (c Config) Validate() error {
	for client, host := range c.Clients {
		if strings.TrimSpace(client) == "" {
			return errors.New(ConfigFile + ": flows.clients has a client with no key")
		}
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("%s: flows.clients names no host for %s", ConfigFile, client)
		}
	}
	return nil
}

type projectFile struct {
	Flows Config `yaml:"flows"`
}

func LoadConfig(dir string) (Config, error) {
	body, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	// Not KnownFields: this reads only the flows group of a file other readers share.
	var file projectFile
	if err := yaml.Unmarshal(body, &file); err != nil {
		return Config{}, fmt.Errorf("%s: %w", filepath.Join(dir, ConfigFile), err)
	}
	return file.Flows, file.Flows.Validate()
}
