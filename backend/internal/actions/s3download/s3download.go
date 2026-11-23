// Package s3download implementiert den Handler-Typ „s3-download“: Er listet die Dateien eines Services in einem
// S3-Bucket (Prefix per Template, z. B. <namespace>/<service>/), bietet sie beim Klick als Mehrfachauswahl an
// (Lazy-Formular-Felder) und liefert die Auswahl als Download (eine Datei direkt, mehrere als ZIP).
package s3download

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/labelselector"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// HandlerType ist der Wert für `handler:` bzw. `action-name:` in der config.yaml.
const HandlerType = "s3-download"

// FieldFiles ist die ID des Formular-Felds mit den gewählten Dateien.
const FieldFiles = "files"

const (
	defaultTimeout = 30 * time.Second
	// maxFiles begrenzt die angebotenen Dateien je Service.
	maxFiles = 1000
)

// settings sind die extra-handler-configs einer Aktion.
type settings struct {
	Endpoint           string              `yaml:"endpoint"`
	Bucket             string              `yaml:"bucket"`
	Prefix             string              `yaml:"prefix"`
	AccessKeyIDEnv     string              `yaml:"access-key-id-env"`
	SecretAccessKeyEnv string              `yaml:"secret-access-key-env"`
	Region             string              `yaml:"region"`
	PathStyle          *bool               `yaml:"path-style"`
	Timeout            string              `yaml:"timeout"`
	ServiceSelectors   []map[string]string `yaml:"service-selectors"`
}

// Handler lädt Dateien eines Services aus einem S3-Bucket herunter.
type Handler struct {
	store     objectStore
	prefix    *template.Template
	selectors []map[string]string
	timeout   time.Duration
}

// New ist die Factory für die Registry.
func New(actions.Deps) actions.Handler { return &Handler{} }

// Configure liest und prüft die extra-handler-configs. Fehler brechen den Start des Backends ab.
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	if err := gitlabpipeline.DecodeExtra(extra, &s); err != nil {
		return err
	}

	var errs []error
	endpoint, secure, err := parseEndpoint(s.Endpoint)
	if err != nil {
		errs = append(errs, err)
	}
	if s.Bucket == "" {
		errs = append(errs, errors.New("bucket fehlt"))
	}
	if s.Prefix == "" {
		errs = append(errs, errors.New("prefix fehlt"))
	} else if t, err := objecttemplate.Parse("prefix", s.Prefix); err != nil {
		errs = append(errs, err)
	} else {
		h.prefix = t
	}
	keyID, err := envValue("access-key-id-env", s.AccessKeyIDEnv)
	if err != nil {
		errs = append(errs, err)
	}
	secret, err := envValue("secret-access-key-env", s.SecretAccessKeyEnv)
	if err != nil {
		errs = append(errs, err)
	}
	h.timeout = defaultTimeout
	if s.Timeout != "" {
		if h.timeout, err = time.ParseDuration(s.Timeout); err != nil || h.timeout <= 0 {
			errs = append(errs, fmt.Errorf("timeout %q ist keine gültige Dauer", s.Timeout))
		}
	}
	errs = append(errs, labelselector.Validate("service-selectors", s.ServiceSelectors)...)
	h.selectors = s.ServiceSelectors
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	pathStyle := s.PathStyle == nil || *s.PathStyle
	h.store, err = newMinioStore(minioConfig{
		Endpoint: endpoint, Secure: secure, Bucket: s.Bucket, Region: s.Region,
		AccessKeyID: keyID, SecretAccessKey: secret, PathStyle: pathStyle,
	})
	return err
}

// IsApplicable gilt für Services, deren Labels zu einem der service-selectors passen.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	svc, ok := obj.Object.(model.StoredService)
	return ok && obj.Ref.Kind == model.KindService && labelselector.MatchesAny(svc.Labels, h.selectors)
}

// FormFields ist leer: Die Dateiliste wird erst beim Klick über LoadFormFields geladen.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// LoadFormFields listet die Dateien des Services und bietet sie als Mehrfachauswahl an.
func (h *Handler) LoadFormFields(ctx context.Context, obj actions.StoredObject) ([]actions.FormField, error) {
	files, err := h.list(ctx, obj)
	if err != nil {
		return nil, err
	}
	field := actions.FormField{
		ID:         FieldFiles,
		Label:      "Dateien",
		Type:       actions.FieldMultiSelect,
		IsRequired: true,
		Options:    []actions.Option{},
	}
	switch {
	case len(files) == 0:
		field.Description = "Zu diesem Service liegen keine Dateien vor"
	case len(files) >= maxFiles:
		field.Description = fmt.Sprintf("Die ersten %d Dateien; eine Datei wird direkt, mehrere werden als ZIP geladen", maxFiles)
	default:
		field.Description = "Eine Datei wird direkt, mehrere werden als ZIP geladen"
	}
	for _, f := range files {
		field.Options = append(field.Options, actions.Option{Value: f.rel, Label: fmt.Sprintf("%s (%s)", f.rel, formatSize(f.Size))})
	}
	return []actions.FormField{field}, nil
}

// Execute wird für Download-Aktionen nicht aufgerufen (die API nutzt Download).
func (h *Handler) Execute(context.Context, actions.StoredObject, map[string]any) actions.Result {
	return actions.Result{Message: "Diese Aktion liefert einen Download"}
}

// Download liefert die gewählten Dateien: bei einer die Datei selbst, bei mehreren ein ZIP.
func (h *Handler) Download(ctx context.Context, obj actions.StoredObject, inputs map[string]any) (actions.File, error) {
	prefix, err := h.renderPrefix(obj)
	if err != nil {
		return actions.File{}, err
	}
	raw, _ := inputs[FieldFiles].([]any)
	var rels []string
	seen := map[string]bool{}
	for _, v := range raw {
		if rel, ok := v.(string); ok && !seen[rel] {
			seen[rel] = true
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		return actions.File{}, &actions.UserError{Message: "Keine Datei ausgewählt"}
	}

	statCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	infos := make([]objectInfo, len(rels))
	for i, rel := range rels {
		info, err := h.store.Stat(statCtx, prefix+rel)
		if err != nil {
			slog.Warn("S3: Datei nicht lesbar", "action", HandlerType, "key", prefix+rel, "error", err)
			return actions.File{}, &actions.UserError{Message: fmt.Sprintf("Datei %s ist nicht lesbar (mehr vorhanden?)", rel)}
		}
		infos[i] = info
	}

	if len(rels) == 1 {
		key := prefix + rels[0]
		ct := infos[0].ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		return actions.File{
			Name:        path.Base(rels[0]),
			ContentType: ct,
			Size:        infos[0].Size,
			Write:       func(w io.Writer) error { return h.copyObject(ctx, key, w) },
		}, nil
	}
	return actions.File{
		Name:        obj.Ref.Name + ".zip",
		ContentType: "application/zip",
		Size:        -1,
		Write: func(w io.Writer) error {
			zw := zip.NewWriter(w)
			for _, rel := range rels {
				ew, err := zw.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: time.Now()})
				if err != nil {
					return err
				}
				if err := h.copyObject(ctx, prefix+rel, ew); err != nil {
					return err
				}
			}
			return zw.Close()
		},
	}, nil
}

func (h *Handler) copyObject(ctx context.Context, key string, w io.Writer) error {
	r, err := h.store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(w, r)
	return err
}

type file struct {
	objectInfo
	// rel ist der Pfad relativ zum Prefix.
	rel string
}

// list liefert die Dateien unter dem Prefix des Objekts (ohne Verzeichnis-Marker und unsichere Pfade).
func (h *Handler) list(ctx context.Context, obj actions.StoredObject) ([]file, error) {
	prefix, err := h.renderPrefix(obj)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	infos, err := h.store.List(ctx, prefix, maxFiles)
	if err != nil {
		slog.Warn("S3: Auflisten fehlgeschlagen", "action", HandlerType, "prefix", prefix, "error", err)
		return nil, &actions.UserError{Message: "Dateien konnten nicht aus dem S3-Bucket gelesen werden"}
	}
	var files []file
	for _, info := range infos {
		rel := strings.TrimPrefix(info.Key, prefix)
		if rel == "" || strings.HasSuffix(rel, "/") || !safeRelPath(rel) {
			continue
		}
		files = append(files, file{objectInfo: info, rel: rel})
	}
	return files, nil
}

func (h *Handler) renderPrefix(obj actions.StoredObject) (string, error) {
	data, err := objecttemplate.NewData(obj.Ref, obj.Object, obj.Project)
	if err != nil {
		return "", fmt.Errorf("objekt-daten nicht lesbar: %w", err)
	}
	p, err := objecttemplate.Render(h.prefix, data)
	if err != nil {
		return "", fmt.Errorf("prefix nicht renderbar: %w", err)
	}
	p = strings.TrimLeft(strings.TrimSpace(p), "/")
	if p == "" {
		return "", errors.New("prefix ist leer")
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p, nil
}

// safeRelPath weist Pfade mit „..“-Segmenten ab, damit ZIP-Einträge nie aus dem Zielordner ausbrechen.
func safeRelPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return false
		}
	}
	return !strings.HasPrefix(rel, "/")
}

func parseEndpoint(raw string) (host string, secure bool, err error) {
	if raw == "" {
		return "", false, errors.New("endpoint fehlt")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return "", false, fmt.Errorf("endpoint %q muss die Form https://host[:port] haben", raw)
	}
	return u.Host, u.Scheme == "https", nil
}

func envValue(field, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%s fehlt", field)
	}
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("umgebungsvariable %s (%s) ist nicht gesetzt", name, field)
	}
	return v, nil
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
