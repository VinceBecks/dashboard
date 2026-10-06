import type { DashboardObject, NamespaceObject, ObjectHeader, ObjectRef, Snapshot } from './types';

/**
 * Wendet die Events von GET /api/events auf den Zustand an. Alle Funktionen sind rein und liefern einen neuen
 * Zustand; unveränderte Teilbäume behalten ihre Identität.
 */

/** upsert: Objekt neu oder geändert. Die Kind-Listen fehlen im Event, vorhandene Kinder bleiben erhalten. */
export function applyUpsert(state: Snapshot, obj: DashboardObject): Snapshot {
  const { project, namespace, service } = obj.parents;
  if (obj.kind === 'namespace') {
    const projects = state.projects.some((p) => p.name === project)
      ? state.projects
      : [...state.projects, { name: project, namespaces: [] }];
    return {
      projects: projects.map((p) =>
        p.name !== project
          ? p
          : {
              ...p,
              namespaces: upsertByName(p.namespaces, obj.name, (old) => ({
                ...obj,
                services: old?.services ?? [],
                ingresses: old?.ingresses ?? [],
              })),
            },
      ),
    };
  }
  return updateNamespace(state, project, namespace, (ns) => {
    switch (obj.kind) {
      case 'service':
        return {
          ...ns,
          services: upsertByName(ns.services ?? [], obj.name, (old) => ({ ...obj, pods: old?.pods ?? [] })),
        };
      case 'ingress':
        return { ...ns, ingresses: upsertByName(ns.ingresses ?? [], obj.name, () => obj) };
      case 'pod':
        return {
          ...ns,
          services: (ns.services ?? []).map((s) =>
            s.name !== service ? s : { ...s, pods: upsertByName(s.pods ?? [], obj.name, () => obj) },
          ),
        };
    }
  });
}

/** delete: Objekt samt aller Kinder entfernen. */
export function applyDelete(state: Snapshot, header: ObjectHeader): Snapshot {
  const { project, namespace, service } = header.parents;
  if (header.kind === 'namespace') {
    return {
      projects: state.projects.map((p) =>
        p.name !== project ? p : { ...p, namespaces: p.namespaces.filter((n) => n.name !== header.name) },
      ),
    };
  }
  return updateNamespace(state, project, namespace, (ns) => {
    switch (header.kind) {
      case 'service':
        return { ...ns, services: (ns.services ?? []).filter((s) => s.name !== header.name) };
      case 'ingress':
        return { ...ns, ingresses: (ns.ingresses ?? []).filter((i) => i.name !== header.name) };
      case 'pod':
        return {
          ...ns,
          services: (ns.services ?? []).map((s) =>
            s.name !== service ? s : { ...s, pods: (s.pods ?? []).filter((p) => p.name !== header.name) },
          ),
        };
    }
    return ns;
  });
}

/** Referenz für PATCH /api/actions/{action}. */
export function refOf(obj: DashboardObject): ObjectRef {
  return { kind: obj.kind, namespace: obj.parents.namespace ?? obj.name, name: obj.name };
}

function updateNamespace(
  state: Snapshot,
  project: string,
  namespace: string | undefined,
  fn: (ns: NamespaceObject) => NamespaceObject,
): Snapshot {
  return {
    projects: state.projects.map((p) =>
      p.name !== project ? p : { ...p, namespaces: p.namespaces.map((n) => (n.name === namespace ? fn(n) : n)) },
    ),
  };
}

/** Ersetzt das Element mit diesem Namen oder fügt es sortiert ein. */
function upsertByName<T extends { name: string }>(list: T[], name: string, next: (old: T | undefined) => T): T[] {
  const index = list.findIndex((e) => e.name === name);
  if (index >= 0) {
    return list.map((e, i) => (i === index ? next(e) : e));
  }
  return [...list, next(undefined)].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}
