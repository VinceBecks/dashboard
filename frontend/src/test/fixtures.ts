import type { NamespaceObject, NodePortServiceObject, PodObject, ServiceArea, ServiceObject, Snapshot } from '../api/types';

export function pod(name: string, ready: boolean, service = 'web'): PodObject {
  return {
    kind: 'pod',
    name,
    parents: { project: 'foo', namespace: 'feat-1', service },
    ready,
    containers: [{ name: 'app', image: 'registry/app:1' }],
    actions: [{ id: 'delete-pod', title: 'Pod löschen', lucideIcon: 'trash', lucideIconColor: 'red' }],
    links: [],
    copyCommands: [
      {
        id: 'shell/app',
        title: 'Shell (app)',
        lucideIcon: 'terminal',
        lucideIconColor: '',
        command: `kubectl exec -it -n feat-1 ${name} -c app -- sh`,
      },
    ],
  };
}

export function service(name: string, running: boolean, pods: PodObject[] = [], areas = ['others']): ServiceObject {
  return {
    kind: 'service',
    name,
    parents: { project: 'foo', namespace: 'feat-1' },
    running,
    areas,
    actions: [],
    links: [],
    copyCommands: [],
    pods,
  };
}

export function nodePortService(name: string, addresses: string[]): NodePortServiceObject {
  return {
    kind: 'node-port-service',
    name,
    parents: { project: 'foo', namespace: 'feat-1' },
    addresses,
    actions: [{ id: 'delete-node-port-service', title: 'NodePort-Service löschen', lucideIcon: 'trash-2', lucideIconColor: 'red' }],
    links: [],
    copyCommands: [],
  };
}

export function namespace(services: ServiceObject[] = [], nodePortServices: NodePortServiceObject[] = []): NamespaceObject {
  return {
    kind: 'namespace',
    name: 'feat-1',
    parents: { project: 'foo' },
    actions: [],
    links: [],
    copyCommands: [],
    services,
    nodePortServices,
    ingresses: [
      {
        kind: 'ingress',
        name: 'web',
        parents: { project: 'foo', namespace: 'feat-1' },
        urls: ['https://feat-1.example/', 'https://feat-1.example/api'],
        actions: [],
        links: [],
    copyCommands: [],
      },
    ],
  };
}

export const serviceAreas: ServiceArea[] = [
  { id: 'business', label: 'Business' },
  { id: 'infra', label: 'Infrastruktur' },
  { id: 'others', label: 'Andere' },
];

export function snapshot(): Snapshot {
  return {
    projects: [
      {
        name: 'foo',
        serviceAreas,
        namespaces: [namespace([service('web', true, [pod('web-1', true)]), service('worker', false)])],
      },
      { name: 'bar', serviceAreas: [{ id: 'others', label: 'Andere' }], namespaces: [] },
    ],
  };
}
