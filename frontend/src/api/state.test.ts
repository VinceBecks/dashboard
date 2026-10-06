import { describe, expect, it } from 'vitest';
import { namespace, pod, service, snapshot } from '../test/fixtures';
import { applyDelete, applyUpsert, refOf } from './state';

const ns = (s: ReturnType<typeof snapshot>) => s.projects[0].namespaces[0];

describe('applyUpsert', () => {
  it('ersetzt einen Service und behält seine Pods', () => {
    const next = applyUpsert(snapshot(), { ...service('web', false), pods: undefined });
    const web = ns(next).services![0];
    expect(web.running).toBe(false);
    expect(web.pods!.map((p) => p.name)).toEqual(['web-1']);
  });

  it('fügt Objekte sortiert ein', () => {
    let next = applyUpsert(snapshot(), service('api', true));
    next = applyUpsert(next, pod('web-0', false));
    expect(ns(next).services!.map((s) => s.name)).toEqual(['api', 'web', 'worker']);
    expect(ns(next).services![1].pods!.map((p) => p.name)).toEqual(['web-0', 'web-1']);
    expect(ns(next).services![0].pods).toEqual([]);
  });

  it('legt neue Namespaces ohne Kinder an und behält bei Updates die Kinder', () => {
    const head = { ...namespace(), services: undefined, ingresses: undefined };
    let next = applyUpsert(snapshot(), { ...head, name: 'feat-0' });
    expect(next.projects[0].namespaces.map((n) => n.name)).toEqual(['feat-0', 'feat-1']);
    expect(next.projects[0].namespaces[0].services).toEqual([]);

    next = applyUpsert(next, { ...head, actions: [{ id: 'x', title: 'X', lucideIcon: 'x', lucideIconColor: '' }] });
    expect(next.projects[0].namespaces[1].actions).toHaveLength(1);
    expect(next.projects[0].namespaces[1].services).toHaveLength(2);
  });

  it('lässt unbeteiligte Projekte unverändert', () => {
    const before = snapshot();
    const next = applyUpsert(before, service('api', true));
    expect(next.projects[1]).toBe(before.projects[1]);
  });
});

describe('applyDelete', () => {
  it('entfernt Pods, Services samt Pods und Namespaces', () => {
    let next = applyDelete(snapshot(), { kind: 'pod', name: 'web-1', parents: { project: 'foo', namespace: 'feat-1', service: 'web' } });
    expect(ns(next).services![0].pods).toEqual([]);
    next = applyDelete(next, { kind: 'service', name: 'web', parents: { project: 'foo', namespace: 'feat-1' } });
    expect(ns(next).services!.map((s) => s.name)).toEqual(['worker']);
    next = applyDelete(next, { kind: 'ingress', name: 'web', parents: { project: 'foo', namespace: 'feat-1' } });
    expect(ns(next).ingresses).toEqual([]);
    next = applyDelete(next, { kind: 'namespace', name: 'feat-1', parents: { project: 'foo' } });
    expect(next.projects[0].namespaces).toEqual([]);
  });
});

describe('refOf', () => {
  it('baut die Objekt-Referenz für Aktionen', () => {
    expect(refOf(namespace())).toEqual({ kind: 'namespace', namespace: 'feat-1', name: 'feat-1' });
    expect(refOf(pod('web-1', true))).toEqual({ kind: 'pod', namespace: 'feat-1', name: 'web-1' });
  });
});
