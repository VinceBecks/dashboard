import type { NamespaceObject, PodObject, ServiceObject, Snapshot } from '../api/types';

export function pod(name: string, ready: boolean, service = 'web'): PodObject {
  return {
    kind: 'pod',
    name,
    parents: { project: 'foo', namespace: 'feat-1', service },
    ready,
    containers: [{ name: 'app', image: 'registry/app:1' }],
    actions: [{ id: 'delete-pod', title: 'Pod löschen', lucideIcon: 'trash', lucideIconColor: 'red' }],
    links: [],
  };
}

export function service(name: string, running: boolean, pods: PodObject[] = []): ServiceObject {
  return {
    kind: 'service',
    name,
    parents: { project: 'foo', namespace: 'feat-1' },
    running,
    actions: [],
    links: [],
    pods,
  };
}

export function namespace(services: ServiceObject[] = []): NamespaceObject {
  return {
    kind: 'namespace',
    name: 'feat-1',
    parents: { project: 'foo' },
    actions: [],
    links: [],
    services,
    ingresses: [
      {
        kind: 'ingress',
        name: 'web',
        parents: { project: 'foo', namespace: 'feat-1' },
        urls: ['https://feat-1.example/', 'https://feat-1.example/api'],
        actions: [],
        links: [],
      },
    ],
  };
}

export function snapshot(): Snapshot {
  return {
    projects: [
      { name: 'foo', namespaces: [namespace([service('web', true, [pod('web-1', true)]), service('worker', false)])] },
      { name: 'bar', namespaces: [] },
    ],
  };
}
