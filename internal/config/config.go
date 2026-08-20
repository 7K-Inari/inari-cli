package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DirName        = "inari"
	ConfigFileName = "config.yaml"
	DefaultServer  = "https://api.inari.dev"
	fileMode       = 0o600
	dirMode        = 0o700
)

type Context struct {
	Server string `yaml:"server"`
	Issuer string `yaml:"issuer,omitempty"`
	Tenant string `yaml:"tenant,omitempty"`
}

type Config struct {
	CurrentContext string             `yaml:"current-context,omitempty"`
	Contexts       map[string]Context `yaml:"contexts,omitempty"`
}

func Dir() (string, error) {
	if d := os.Getenv("INARI_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(base, DirName), nil
}

func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, ConfigFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *Config) SetContext(name string, ctx Context) {
	if c.Contexts == nil {
		c.Contexts = map[string]Context{}
	}
	c.Contexts[name] = ctx
}

func (c *Config) Current() (string, Context, error) {
	if c.CurrentContext == "" {
		return "", Context{}, errors.New("no current context set; run 'inari login' or 'inari context use <name>'")
	}
	ctx, ok := c.Contexts[c.CurrentContext]
	if !ok {
		return "", Context{}, fmt.Errorf("current context %q not found in config", c.CurrentContext)
	}
	return c.CurrentContext, ctx, nil
}
