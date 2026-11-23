package s3download

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
)

type fakeStore struct {
	objects map[string]string
	listErr error
}

func (f *fakeStore) List(_ context.Context, prefix string, limit int) ([]objectInfo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var infos []objectInfo
	for _, k := range keys {
		infos = append(infos, objectInfo{Key: k, Size: int64(len(f.objects[k]))})
	}
	return infos, nil
}

func (f *fakeStore) Stat(_ context.Context, key string) (objectInfo, error) {
	c, ok := f.objects[key]
	if !ok {
		return objectInfo{}, errors.New("not found")
	}
	return objectInfo{Key: key, Size: int64(len(c)), ContentType: "text/plain"}, nil
}

func (f *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.objects[key])), nil
}

func validExtra() map[string]any {
	return map[string]any{
		"endpoint":              "https://s3.example.com:9000",
		"bucket":                "files",
		"prefix":                "{{ .Namespace }}/{{ .Name }}",
		"access-key-id-env":     "S3_TEST_ID",
		"secret-access-key-env": "S3_TEST_SECRET",
		"service-selectors": []any{
			map[string]any{"app": "shop", "tier": "backend"},
			map[string]any{"app": "billing"},
		},
	}
}

func newHandler(t *testing.T, objects map[string]string) *Handler {
	t.Helper()
	t.Setenv("S3_TEST_ID", "id")
	t.Setenv("S3_TEST_SECRET", "secret")
	h := New(actions.Deps{}).(*Handler)
	if err := h.Configure(validExtra()); err != nil {
		t.Fatal(err)
	}
	h.store = &fakeStore{objects: objects}
	return h
}

func service(labels map[string]string) actions.StoredObject {
	return actions.StoredObject{
		Ref:     model.ObjectRef{Kind: model.KindService, Namespace: "ns", Name: "svc"},
		Object:  model.StoredService{Meta: model.Meta{Labels: labels}},
		Project: "foo",
	}
}

func TestConfigureErrors(t *testing.T) {
	t.Setenv("S3_TEST_ID", "id")
	t.Setenv("S3_TEST_SECRET", "secret")
	cases := map[string]func(m map[string]any){
		"endpoint fehlt":          func(m map[string]any) { delete(m, "endpoint") },
		"endpoint ohne Schema":    func(m map[string]any) { m["endpoint"] = "s3.example.com" },
		"bucket fehlt":            func(m map[string]any) { delete(m, "bucket") },
		"prefix fehlt":            func(m map[string]any) { delete(m, "prefix") },
		"prefix-template defekt":  func(m map[string]any) { m["prefix"] = "{{ .Namespace" },
		"env-name fehlt":          func(m map[string]any) { delete(m, "access-key-id-env") },
		"env nicht gesetzt":       func(m map[string]any) { m["secret-access-key-env"] = "S3_TEST_UNSET" },
		"selectors fehlen":        func(m map[string]any) { delete(m, "service-selectors") },
		"leerer selector-eintrag": func(m map[string]any) { m["service-selectors"] = []any{map[string]any{}} },
		"timeout ungültig":        func(m map[string]any) { m["timeout"] = "viel" },
	}
	for name, mutate := range cases {
		m := validExtra()
		mutate(m)
		if err := New(actions.Deps{}).Configure(m); err == nil {
			t.Errorf("%s: Fehler erwartet", name)
		}
	}
}

func TestIsApplicable(t *testing.T) {
	h := newHandler(t, nil)
	cases := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"UND erfüllt", map[string]string{"app": "shop", "tier": "backend"}, true},
		{"ODER zweiter Eintrag", map[string]string{"app": "billing"}, true},
		{"UND unvollständig", map[string]string{"app": "shop"}, false},
		{"ohne Labels", nil, false},
	}
	for _, c := range cases {
		if got := h.IsApplicable(service(c.labels)); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	ns := actions.StoredObject{Ref: model.ObjectRef{Kind: model.KindNamespace, Name: "ns"}, Object: model.StoredNamespace{}}
	if h.IsApplicable(ns) {
		t.Error("Namespace darf nicht anwendbar sein")
	}
}

func TestLoadFormFields(t *testing.T) {
	h := newHandler(t, map[string]string{
		"ns/svc/a.txt":       "aaa",
		"ns/svc/sub/b.txt":   "bb",
		"ns/svc/sub/":        "",
		"ns/svc/../evil.txt": "x",
		"ns/other/c.txt":     "c",
		"ns/svc-other/d.txt": "d",
	})
	fields, err := h.LoadFormFields(context.Background(), service(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].Type != actions.FieldMultiSelect || !fields[0].IsRequired {
		t.Fatalf("fields: %+v", fields)
	}
	var values []string
	for _, o := range fields[0].Options {
		values = append(values, o.Value)
	}
	if want := []string{"a.txt", "sub/b.txt"}; !slices.Equal(values, want) {
		t.Errorf("options: got %v, want %v", values, want)
	}
	if got := fields[0].Options[0].Label; got != "a.txt (3 B)" {
		t.Errorf("label: %q", got)
	}
	if h.FormFields(service(nil)) != nil {
		t.Error("FormFields muss nil sein (lazy)")
	}
}

func TestLoadFormFieldsStoreError(t *testing.T) {
	h := newHandler(t, nil)
	h.store = &fakeStore{listErr: errors.New("boom")}
	_, err := h.LoadFormFields(context.Background(), service(nil))
	var ue *actions.UserError
	if !errors.As(err, &ue) || strings.Contains(ue.Message, "boom") {
		t.Errorf("UserError ohne interne Details erwartet, got %v", err)
	}
}

func read(t *testing.T, f actions.File) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadSingleFile(t *testing.T) {
	h := newHandler(t, map[string]string{"ns/svc/sub/b.txt": "hello"})
	f, err := h.Download(context.Background(), service(nil), map[string]any{FieldFiles: []any{"sub/b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "b.txt" || f.ContentType != "text/plain" || f.Size != 5 {
		t.Errorf("file: %+v", f)
	}
	if got := string(read(t, f)); got != "hello" {
		t.Errorf("content: %q", got)
	}
}

func TestDownloadZip(t *testing.T) {
	h := newHandler(t, map[string]string{"ns/svc/a.txt": "aaa", "ns/svc/sub/b.txt": "bb"})
	f, err := h.Download(context.Background(), service(nil),
		map[string]any{FieldFiles: []any{"a.txt", "sub/b.txt", "a.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "svc.zip" || f.ContentType != "application/zip" || f.Size != -1 {
		t.Errorf("file: %+v", f)
	}
	data := read(t, f)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range zr.File {
		r, _ := e.Open()
		b, _ := io.ReadAll(r)
		got[e.Name] = string(b)
	}
	if len(got) != 2 || got["a.txt"] != "aaa" || got["sub/b.txt"] != "bb" {
		t.Errorf("zip: %v", got)
	}
}

func TestDownloadMissingFile(t *testing.T) {
	h := newHandler(t, map[string]string{"ns/svc/a.txt": "aaa"})
	_, err := h.Download(context.Background(), service(nil), map[string]any{FieldFiles: []any{"a.txt", "gone.txt"}})
	var ue *actions.UserError
	if !errors.As(err, &ue) {
		t.Errorf("UserError erwartet, got %v", err)
	}
}
