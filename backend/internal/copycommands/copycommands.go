// Package copycommands rendert die in der config.yaml konfigurierten Kopier-Befehle (z. B. kubectl exec) je Objekt.
package copycommands

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"text/template"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// Command ist ein fertig gerenderter Befehl, den das Frontend in die Zwischenablage kopiert.
type Command struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Icon      string `json:"icon"`
	IconColor string `json:"iconColor"`
	Command   string `json:"command"`
}

type compiled struct {
	cfg      config.CopyCommand
	commands map[model.Kind]*template.Template
}

// Registry hält die beim Start geparsten Befehls-Templates.
type Registry struct {
	commands    []compiled
	globalVars  map[string]string
	projectVars map[string]map[string]string
}

// NewRegistry parst alle Befehls-Templates. Fehler brechen den Start des Backends ab.
func NewRegistry(cfg *config.Config) (*Registry, error) {
	r := &Registry{globalVars: cfg.LinkVariables, projectVars: map[string]map[string]string{}}
	for _, p := range cfg.Projects {
		r.projectVars[p.Name] = p.LinkVariablesOverrides
	}
	var errs []error
	for _, c := range cfg.CopyCommands {
		if len(c.Projects) == 0 {
			slog.Warn("Kopier-Befehl ist in keinem Projekt verfügbar (projects ist leer)", "command", c.ID)
		}
		cc := compiled{cfg: c, commands: map[model.Kind]*template.Template{}}
		for kind, target := range c.Targets {
			t, err := objecttemplate.Parse(fmt.Sprintf("copy-commands.%s.targets.%s.command", c.ID, kind), target.Command)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			cc.commands[kind] = t
		}
		r.commands = append(r.commands, cc)
	}
	return r, errors.Join(errs...)
}

// For liefert die Befehle zu einem Objekt (nie nil). obj ist ein model.Stored*-Wert, project sein Projekt.
// Bei Pods wird je Container gerendert: Nutzt das Template .Container, entsteht je Container ein eigener Befehl.
// Befehle, die sich für dieses Objekt nicht rendern lassen, werden weggelassen und geloggt.
func (r *Registry) For(ref model.ObjectRef, obj any, project string) []Command {
	result := []Command{}
	if r == nil {
		return result
	}
	data, err := objecttemplate.NewData(ref, obj, project)
	if err != nil {
		slog.Warn("Kopier-Befehle nicht erzeugbar", "object", ref, "error", err)
		return result
	}
	maps.Copy(data.Vars, r.globalVars)
	maps.Copy(data.Vars, r.projectVars[project])
	containers := []string{""}
	if pod, ok := obj.(model.StoredPod); ok && len(pod.Containers) > 0 {
		containers = containers[:0]
		for _, c := range pod.Containers {
			containers = append(containers, c.Name)
		}
	}

	for _, c := range r.commands {
		tpl, ok := c.commands[ref.Kind]
		if !ok || !slices.Contains(c.cfg.Projects, project) {
			continue
		}
		rendered, err := renderPerContainer(tpl, data, containers)
		if err != nil {
			slog.Warn("Kopier-Befehl konnte nicht erzeugt werden", "command", c.cfg.ID, "object", ref, "error", err)
			continue
		}
		result = append(result, commands(c.cfg, containers, rendered)...)
	}
	return result
}

// renderPerContainer rendert das Template je Container (Reihenfolge wie containers).
func renderPerContainer(tpl *template.Template, data objecttemplate.Data, containers []string) ([]string, error) {
	rendered := make([]string, 0, len(containers))
	for _, name := range containers {
		data.Container = name
		cmd, err := objecttemplate.Render(tpl, data)
		if err != nil {
			return nil, err
		}
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return nil, errors.New("befehl ist leer")
		}
		rendered = append(rendered, cmd)
	}
	return rendered, nil
}

// commands baut aus den gerenderten Befehlen die Ausgabe: einer, wenn alle gleich sind (das Template nutzt
// .Container nicht), sonst je Container einer mit Container-Name in ID und Titel.
func commands(cfg config.CopyCommand, containers, rendered []string) []Command {
	base := Command{ID: cfg.ID, Title: cfg.Title, Icon: cfg.Icon, IconColor: cfg.IconColor}
	if !slices.ContainsFunc(rendered, func(c string) bool { return c != rendered[0] }) {
		base.Command = rendered[0]
		return []Command{base}
	}
	result := make([]Command, 0, len(rendered))
	for i, cmd := range rendered {
		c := base
		c.ID = cfg.ID + "/" + containers[i]
		c.Title = cfg.Title + " (" + containers[i] + ")"
		c.Command = cmd
		result = append(result, c)
	}
	return result
}
