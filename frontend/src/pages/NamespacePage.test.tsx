import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Action, NamespaceResponse } from '../api/types';
import { mockFetch } from '../test/fetchMock';
import { NamespacePage } from './NamespacePage';

afterEach(() => vi.unstubAllGlobals());

const action = (id: string, title: string): Action => ({ id, title, lucideIcon: 'trash', lucideIconColor: 'red' });

const data: NamespaceResponse = {
  namespace: { name: 'feat-1', project: 'foo', actions: [action('ns-action', 'Namespace löschen')] },
  ingresses: [
    {
      name: 'web',
      actions: [],
      rules: [
        {
          host: 'web.example',
          paths: [{ path: '/', pathType: 'Prefix', backend: { serviceName: 'web', servicePortName: 'http', resolvedServicePortNumber: 80 } }],
        },
      ],
    },
  ],
  services: [
    {
      name: 'web',
      type: 'ClusterIP',
      ports: [{ name: 'http', port: 80, targetPort: 'http', resolvedTargetPort: 8080 }],
      replicas: ['web-1'],
      actions: [action('svc-action', 'Service neu starten')],
    },
    {
      name: 'debug',
      type: 'NodePort',
      ports: [{ port: 80, targetPort: '80', nodePort: 30080 }],
      replicas: [],
      actions: [],
    },
  ],
  pods: [
    {
      name: 'web-1',
      phase: 'Running',
      ready: true,
      containers: [{ name: 'web', image: 'nginx:1.27' }],
      actions: [action('delete-pod', 'Pod löschen')],
    },
  ],
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/projects/foo/namespaces/feat-1']}>
      <Routes>
        <Route path="/projects/:project/namespaces/:namespace" element={<NamespacePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('NamespacePage', () => {
  it('zeigt Aktionen, Ingresses, NodePort- und Anwendungs-Services', async () => {
    const fetchMock = mockFetch(() => ({ status: 200, body: data, etag: '"1"' }));
    renderPage();

    expect(await screen.findByRole('heading', { name: 'feat-1' })).toBeInTheDocument();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/projects/foo/namespaces/feat-1');
    expect(screen.getByRole('button', { name: 'Namespace löschen' })).toBeInTheDocument();

    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(['Ingresses', 'NodePort-Services', 'Anwendungs-Services']);

    expect(screen.getByText('web.example')).toBeInTheDocument();
    expect(screen.getByText('web:80')).toBeInTheDocument();

    const nodePorts = screen.getByRole('heading', { name: 'NodePort-Services' }).parentElement!;
    expect(within(nodePorts).getByText('debug')).toBeInTheDocument();
    expect(within(nodePorts).getByText('80 → 30080')).toBeInTheDocument();

    const apps = screen.getByRole('heading', { name: 'Anwendungs-Services' }).parentElement!;
    expect(within(apps).queryByText('debug')).not.toBeInTheDocument();
    expect(within(apps).getByText('http 80→8080')).toBeInTheDocument();
    expect(within(apps).getByRole('button', { name: 'Service neu starten' })).toBeInTheDocument();
    expect(within(apps).getByText('web-1')).toBeInTheDocument();
    expect(within(apps).getByText('Ready')).toBeInTheDocument();

    // Pod-Aktionen werden vorerst nicht angezeigt.
    expect(screen.queryByRole('button', { name: 'Pod löschen' })).not.toBeInTheDocument();
  });

  it('meldet einen nicht (mehr) existierenden Namespace', async () => {
    mockFetch(() => ({ status: 404, body: { error: 'namespace nicht gefunden' } }));
    renderPage();
    expect(await screen.findByText(/existiert nicht \(mehr\)/)).toBeInTheDocument();
  });
});
