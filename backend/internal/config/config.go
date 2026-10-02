// Package config lädt und validiert die config.yaml des Dashboards.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config ist die beim Startup eingelesene Konfiguration.
type Config struct {
	Projects       []Project             `yaml:"projects"`
	ActionHandlers []ActionHandlerConfig `yaml:"actionHandlers"`
	Store          StoreConfig           `yaml:"store"`
}

// Project definiert ein Projekt und die Labels, die ein Namespace tragen muss, um dazuzugehören.
type Project struct {
	Name            string            `yaml:"project-name"`
	NamespaceLabels map[string]string `yaml:"namespace-labels"`
}

// ActionHandlerConfig konfiguriert genau eine Aktion und damit genau eine ActionHandler-Instanz.
type ActionHandlerConfig struct {
	Name string `yaml:"action-name"`
	// Handler ist der Handler-Typ. Ein Typ kann mehrere Aktionen bedienen (z. B. gitlab-pipeline).
	// Fehlt er, gilt der action-name als Handler-Typ.
	Handler         string         `yaml:"handler"`
	Enabled         bool           `yaml:"enabled"`
	Title           string         `yaml:"action-title"`
	LucideIcon      string         `yaml:"lucide-icon"`
	LucideIconColor string         `yaml:"lucide-icon-color"`
	Extra           map[string]any `yaml:"extra-handler-configs"`
}

// HandlerType liefert den Handler-Typ der Aktion.
func (a ActionHandlerConfig) HandlerType() string {
	if a.Handler != "" {
		return a.Handler
	}
	return a.Name
}

// StoreConfig konfiguriert den In-Memory-Store.
type StoreConfig struct {
	RecreateInterval Interval `yaml:"recreate-store-interval"`
}

// Interval ist eine Dauer in Minuten (m), Stunden (h) oder Tagen (d). 0 bedeutet deaktiviert.
type Interval time.Duration

// Duration liefert das Interval als time.Duration.
func (i Interval) Duration() time.Duration { return time.Duration(i) }

// UnmarshalYAML parst Werte wie "0", "30m", "2h" oder "1d".
func (i *Interval) UnmarshalYAML(node *yaml.Node) error {
	d, err := ParseInterval(node.Value)
	if err != nil {
		return err
	}
	*i = Interval(d)
	return nil
}

// ParseInterval parst "0" oder "<n>m", "<n>h", "<n>d".
func ParseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("ungültiges Interval %q: erwartet 0 oder <Zahl>m|h|d", s)
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("ungültiges Interval %q: erwartet 0 oder <Zahl>m|h|d", s)
}

// Load liest die Konfiguration aus der angegebenen Datei.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config lesen: %w", err)
	}
	return Parse(data)
}

// Parse dekodiert und validiert die Konfiguration.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config parsen: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate prüft die Konfiguration auf Konsistenz.
func (c *Config) Validate() error {
	var errs []error
	projects := map[string]bool{}
	for i, p := range c.Projects {
		if p.Name == "" {
			errs = append(errs, fmt.Errorf("projects[%d]: project-name fehlt", i))
		} else if projects[p.Name] {
			errs = append(errs, fmt.Errorf("projects[%d]: project-name %q ist nicht eindeutig", i, p.Name))
		}
		projects[p.Name] = true
		if len(p.NamespaceLabels) == 0 {
			errs = append(errs, fmt.Errorf("projects[%d]: namespace-labels dürfen nicht leer sein", i))
		}
	}
	actions := map[string]bool{}
	for i, a := range c.ActionHandlers {
		if a.Name == "" {
			errs = append(errs, fmt.Errorf("actionHandlers[%d]: action-name fehlt", i))
		} else if actions[a.Name] {
			errs = append(errs, fmt.Errorf("actionHandlers[%d]: action-name %q ist nicht eindeutig", i, a.Name))
		}
		actions[a.Name] = true
	}
	return errors.Join(errs...)
}
