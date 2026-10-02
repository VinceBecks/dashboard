// Package config lädt und validiert die config.yaml des Dashboards.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"

	"feature-dashboard/internal/model"
)

// Config ist die beim Startup eingelesene Konfiguration.
type Config struct {
	Projects       []Project             `yaml:"projects"`
	ActionHandlers []ActionHandlerConfig `yaml:"actionHandlers"`
	Store          StoreConfig           `yaml:"store"`
	Cluster        ClusterConfig         `yaml:"cluster"`
	// LinkVariables sind statische Werte für Link-Templates (.Vars); je Projekt per link-variables-overrides
	// überschreibbar.
	LinkVariables map[string]string `yaml:"link-variables"`
	Links         []Link            `yaml:"links"`
}

// ClusterConfig enthält Angaben zum Cluster, die das Dashboard anzeigt.
type ClusterConfig struct {
	// DNSName ist der DNS-Name (oder die IP), unter dem die Nodes erreichbar sind. Er gilt für alle
	// NodePort-Services: <dns-name>:<node-port>. Optional.
	DNSName string `yaml:"dns-name"`
}

// Link ist ein konfigurierbarer Absprung (z. B. nach Kibana), den das Dashboard an Objekten anzeigt.
type Link struct {
	ID              string `yaml:"link-id"`
	Title           string `yaml:"title"`
	LucideIcon      string `yaml:"lucide-icon"`
	LucideIconColor string `yaml:"lucide-icon-color"`
	// Projects (Pflicht) sind die Projekte, in denen der Link angeboten wird; leer heißt: in keinem.
	Projects []string `yaml:"projects"`
	// Targets: je Objekt-Typ (namespace, service, ingress, pod) die Ziel-URL. Nur für die enthaltenen Typen wird
	// der Link angezeigt.
	Targets map[model.Kind]LinkTarget `yaml:"targets"`
}

// LinkTarget ist das Ziel eines Links für einen Objekt-Typ.
type LinkTarget struct {
	// URL ist ein Go-Template; Zeilenumbrüche samt Einrückung werden vor dem Parsen entfernt.
	// Werte kodiert das Template selbst (urlquery, pathescape, rison).
	URL string `yaml:"url"`
}

// Project definiert ein Projekt und die Labels, die ein Namespace tragen muss, um dazuzugehören.
type Project struct {
	Name            string            `yaml:"project-name"`
	NamespaceLabels map[string]string `yaml:"namespace-labels"`
	// ServiceAreas teilen die Anwendungs-Services im Dashboard in Bereiche auf.
	ServiceAreas []ServiceArea `yaml:"service-areas"`
	// OthersAreaLabel ist das Label des Bereichs für nicht zugeordnete Services (siehe OthersLabel).
	OthersAreaLabel string `yaml:"others-area-label"`
	// LinkVariablesOverrides überschreiben bzw. ergänzen die globalen link-variables für dieses Projekt.
	LinkVariablesOverrides map[string]string `yaml:"link-variables-overrides"`
}

// OthersAreaName ist die reservierte ID des Bereichs für Services, die keinem Bereich zugeordnet sind.
const OthersAreaName = "others"

// OthersLabel liefert das Label des others-Bereichs. Ohne konfigurierte Bereiche heißt er „Anwendungs-Services“,
// da er dann alle Anwendungs-Services enthält.
func (p Project) OthersLabel() string {
	switch {
	case p.OthersAreaLabel != "":
		return p.OthersAreaLabel
	case len(p.ServiceAreas) == 0:
		return "Anwendungs-Services"
	default:
		return "Andere"
	}
}

// ServiceArea ist ein Bereich von Anwendungs-Services. Ein Service gehört dazu, wenn er alle Labels mindestens
// einer LabelCombination trägt (ODER über die Kombinationen, UND innerhalb einer Kombination).
type ServiceArea struct {
	Name              string              `yaml:"area-name"`
	Label             string              `yaml:"area-label"`
	LabelCombinations []map[string]string `yaml:"label-combinations"`
}

// ActionHandlerConfig konfiguriert genau eine Aktion und damit genau eine ActionHandler-Instanz.
type ActionHandlerConfig struct {
	Name string `yaml:"action-name"`
	// Handler ist der Handler-Typ. Ein Typ kann mehrere Aktionen bedienen (z. B. gitlab-pipeline).
	// Fehlt er, gilt der action-name als Handler-Typ.
	Handler         string `yaml:"handler"`
	Enabled         bool   `yaml:"enabled"`
	Title           string `yaml:"action-title"`
	LucideIcon      string `yaml:"lucide-icon"`
	LucideIconColor string `yaml:"lucide-icon-color"`
	// Projects (Pflicht bei enabled: true) sind die Projekte, in denen die Aktion angeboten wird; leer heißt: in keinem.
	Projects []string       `yaml:"projects"`
	Extra    map[string]any `yaml:"extra-handler-configs"`
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
		errs = append(errs, validateServiceAreas(fmt.Sprintf("projects[%d]", i), p.ServiceAreas)...)
	}
	errs = append(errs, validateLinks(c.Links, projects)...)
	if d := c.Cluster.DNSName; d != "" && net.ParseIP(d) == nil && len(validation.IsDNS1123Subdomain(strings.ToLower(d))) > 0 {
		errs = append(errs, fmt.Errorf("cluster.dns-name %q ist weder ein gültiger DNS-Name noch eine IP (ohne Schema und Port angeben)", d))
	}
	actions := map[string]bool{}
	for i, a := range c.ActionHandlers {
		if a.Name == "" {
			errs = append(errs, fmt.Errorf("actionHandlers[%d]: action-name fehlt", i))
		} else if actions[a.Name] {
			errs = append(errs, fmt.Errorf("actionHandlers[%d]: action-name %q ist nicht eindeutig", i, a.Name))
		}
		actions[a.Name] = true
		if a.Enabled {
			errs = append(errs, validateProjects(fmt.Sprintf("actionHandlers[%d]", i), a.Projects, projects)...)
		}
	}
	return errors.Join(errs...)
}

func validateServiceAreas(prefix string, areas []ServiceArea) []error {
	var errs []error
	names := map[string]bool{}
	for i, a := range areas {
		at := fmt.Sprintf("%s.service-areas[%d]", prefix, i)
		switch {
		case a.Name == "":
			errs = append(errs, fmt.Errorf("%s: area-name fehlt", at))
		case a.Name == OthersAreaName:
			errs = append(errs, fmt.Errorf("%s: area-name %q ist reserviert", at, OthersAreaName))
		case names[a.Name]:
			errs = append(errs, fmt.Errorf("%s: area-name %q ist nicht eindeutig", at, a.Name))
		}
		names[a.Name] = true
		if a.Label == "" {
			errs = append(errs, fmt.Errorf("%s: area-label fehlt", at))
		}
		if len(a.LabelCombinations) == 0 {
			errs = append(errs, fmt.Errorf("%s: label-combinations dürfen nicht leer sein", at))
		}
		for j, c := range a.LabelCombinations {
			if len(c) == 0 {
				errs = append(errs, fmt.Errorf("%s.label-combinations[%d]: leere Kombination würde alle Services treffen", at, j))
			}
		}
	}
	return errs
}

func validateLinks(links []Link, projects map[string]bool) []error {
	var errs []error
	ids := map[string]bool{}
	for i, l := range links {
		at := fmt.Sprintf("links[%d]", i)
		switch {
		case l.ID == "":
			errs = append(errs, fmt.Errorf("%s: link-id fehlt", at))
		case ids[l.ID]:
			errs = append(errs, fmt.Errorf("%s: link-id %q ist nicht eindeutig", at, l.ID))
		}
		ids[l.ID] = true
		if l.Title == "" {
			errs = append(errs, fmt.Errorf("%s: title fehlt", at))
		}
		errs = append(errs, validateProjects(at, l.Projects, projects)...)
		if len(l.Targets) == 0 {
			errs = append(errs, fmt.Errorf("%s: targets dürfen nicht leer sein", at))
		}
		for kind, target := range l.Targets {
			if !kind.Valid() {
				errs = append(errs, fmt.Errorf("%s: targets: ungültiger Objekt-Typ %q", at, kind))
			}
			if target.URL == "" {
				errs = append(errs, fmt.Errorf("%s: targets.%s: url fehlt (Typ ohne Link einfach weglassen)", at, kind))
			}
		}
	}
	return errs
}

// validateProjects prüft das Pflichtfeld projects von Links und Aktionen: Es muss gesetzt sein (eine leere Liste
// ist erlaubt und bedeutet „in keinem Projekt“) und darf nur bekannte Projekte enthalten.
func validateProjects(at string, projects []string, known map[string]bool) []error {
	if projects == nil {
		return []error{fmt.Errorf("%s: projects fehlt (leere Liste [] heißt: in keinem Projekt)", at)}
	}
	var errs []error
	for _, p := range projects {
		if !known[p] {
			errs = append(errs, fmt.Errorf("%s: unbekanntes Projekt %q", at, p))
		}
	}
	return errs
}
