Ich möchte das verhalten bei den links vereinfachen.
jeder target-type definiert direkt die URLs und kann jeweils `query-params` oder `query-params-url-encoded` angeben.
Verliert die Logik dadurch an Funktionalität? 

Zusätzlich habe ich das attribut `link-variables` unter den projekten umbenannt zu `link-variables-overrides`

```yaml
projects:
  - project-name: foo-projekt
    namespace-labels: { ... }
    link-variables-overrides:     # (umbenannt) überschreibt/ergänzt die globalen Werte für dieses Projekt
      fooUrl: https://other.mycomp.de


# Globale, statische Link-Variablen; überschreibbar pro projekt
link-variables:
  fooUrl: https://foo.mycomp.de

links:
  - link-id: foo-system                 # eindeutig
    title: Absprung nach Foo                # Tooltip im Dashboard
    lucide-icon: scroll-text
    lucide-icon-color: "#00bfb3"
    projects: [foo-projekt]              # required feld. wenn empty, dann ist der link in keinem projekt verfügbar
    # Je Objekt-Typ ein eigenes Ziel. Fehlt ein Typ, gibt es den Link dort nicht.
    targets:
      namespace:
        url: "{{ .Vars.fooUrl }}/{{ .Namespace }}"
        query-params: # werte werden nicht url-encoded
          - key: foo
            value: bar
        query-params-url-encoded: # werte werden automatisch url-encoded
          - key: foobar
            value: {{ .Namespace }}
      pod: 
        url: "{{ .Vars.fooUrl }}/{{ .Namespace }}/{{ .Pod }}"
        query-params: # werte werden nicht url-encoded
          - key: foo
            value: bar
        query-params-url-encoded: # werte werden automatisch url-encoded
          - key: foobar
            value: {{ .Pod }}
      service: {} # nicht verfügbar bei services
```