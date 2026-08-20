package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/7K-Inari/inari-cli/internal/config"
)

// Cache persists per-context tokens under <config dir>/tokens/<context>.json (0600).
type Cache struct {
	Dir string
}

func NewCache() (*Cache, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return &Cache{Dir: filepath.Join(dir, "tokens")}, nil
}

func (c *Cache) path(contextName string) string {
	return filepath.Join(c.Dir, contextName+".json")
}

func (c *Cache) Load(contextName string) (*Token, error) {
	data, err := os.ReadFile(c.path(contextName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading token cache: %w", err)
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parsing token cache: %w", err)
	}
	return &tok, nil
}

func (c *Cache) Save(contextName string, tok *Token) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return fmt.Errorf("creating token dir: %w", err)
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.path(contextName), data, 0o600); err != nil {
		return fmt.Errorf("writing token cache: %w", err)
	}
	return nil
}

func (c *Cache) Delete(contextName string) error {
	err := os.Remove(c.path(contextName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing token cache: %w", err)
	}
	return nil
}
