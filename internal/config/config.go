// Package config manages the host-side NexoraCLI configuration: a TOML file holding
// one or more named instances (each with a URL + saved tokens) and which one is current.
//
// Location: <os.UserConfigDir()>/nexora/config.toml
//   linux:   ~/.config/nexora/config.toml
//   macOS:   ~/Library/Application Support/nexora/config.toml
//   windows: %AppData%\nexora\config.toml
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Instance is a single Nexora deployment the CLI can talk to.
type Instance struct {
	URL          string `toml:"url"`           // base URL, e.g. https://nexora.example.com (no /api)
	AccessToken  string `toml:"access_token"`  // JWT (auto-refreshed)
	RefreshToken string `toml:"refresh_token"` // JWT refresh token
	APIKey       string `toml:"api_key"`       // optional nxr_ key (takes precedence if set)
	UserEmail    string `toml:"user_email"`
	UserName     string `toml:"user_name"`
}

// Config is the whole file.
type Config struct {
	Current   string               `toml:"current"`
	Instances map[string]*Instance `toml:"instances"`

	// Local execution preferences (persist across sessions so the user doesn't re-toggle).
	LocalExec bool `toml:"local_exec"`
	LocalYolo bool `toml:"local_yolo"`

	// UIMode controls TUI complexity: "simple" hides advanced tabs, "advanced" shows all.
	UIMode string `toml:"ui_mode"`

	path string `toml:"-"`
}

// Path returns the resolved config file path (honors NEXORA_CONFIG override).
func Path() (string, error) {
	if p := os.Getenv("NEXORA_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nexora", "config.toml"), nil
}

// Load reads the config, returning an empty (but valid) config if none exists yet.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	c := &Config{Instances: map[string]*Instance{}, path: p}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, c); err != nil {
		// Corrupted config (e.g. an interrupted/concurrent write before atomic saves).
		// Don't brick the CLI: back up the bad file and start fresh so the user can
		// re-login. Saved instances are lost, but the file was unusable anyway.
		_ = os.WriteFile(p+".corrupt", data, 0o600)
		return &Config{Instances: map[string]*Instance{}, path: p}, nil
	}
	if c.Instances == nil {
		c.Instances = map[string]*Instance{}
	}
	c.path = p
	return c, nil
}

// Save writes the config back to disk (0600 — it holds tokens).
func (c *Config) Save() error {
	if c.path == "" {
		p, err := Path()
		if err != nil {
			return err
		}
		c.path = p
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	// Atomic write: encode to a temp file in the same dir, then rename over the target.
	// A crash or concurrent writer can never leave a half-written config.toml (which
	// would corrupt the saved tokens and break auth).
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeded
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.path)
}

// CurrentInstance returns the active instance, or nil if none selected/configured.
func (c *Config) CurrentInstance() *Instance {
	if c.Current == "" {
		return nil
	}
	return c.Instances[c.Current]
}

// Set adds or replaces an instance and makes it current.
func (c *Config) Set(name string, inst *Instance) {
	name = normalizeName(name)
	if c.Instances == nil {
		c.Instances = map[string]*Instance{}
	}
	c.Instances[name] = inst
	c.Current = name
}

// Names returns the configured instance names.
func (c *Config) Names() []string {
	out := make([]string, 0, len(c.Instances))
	for k := range c.Instances {
		out = append(out, k)
	}
	return out
}

func normalizeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "default"
	}
	return s
}
