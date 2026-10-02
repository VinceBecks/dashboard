import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Action, Link, NamespaceResponse } from '../api/types';
import { mockFetch } from '../test/fetchMock';
import { NamespacePage } from './NamespacePage';

afterEach(() => vi.unstubAllGlobals());

const action = (id: string, title: string): Action => ({ id, title, lucideIcon: 'trash', lucideIconColor: 'red' });

const link = (id: string, title: string, url: string): Link => ({ id, title, url, lucideIcon: 'scroll-text', lucideIconColor: 'teal' });

const data: NamespaceResponse = {
  namespace: {
    name: 'feat-1',
    project: 'foo',
    actions: [action('ns-action', 'Namespace löschen')],
    links: [link('logs', 'Namespace-Logs', 'https://kibana.example/ns')],
  },
  ingresses: [
    {
      name: 'web',
      actions: [],
      links: [],
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
      links: [],
    },
    {
      name: 'debug',
      type: 'NodePort',
      ports: [{ port: 80, targetPort: '80', nodePort: 30080 }],
      replicas: [],
      actions: [],
      links: [],
    },
  ],
  serviceAreas: [
    { id: 'business', label: 'Business', services: ['web'] },
    { id: 'infra', label: 'Infrastruktur', services: [] },
    { id: 'others', label: 'Andere', services: ['web'] },
  ],
  pods: [
    {
      name: 'web-1',
      phase: 'Running',
      ready: true,
      containers: [{ name: 'web', image: 'nginx:1.27' }],
      actions: [action('delete-pod', 'Pod löschen')],
      links: [link('logs', 'Pod-Logs', 'https://kibana.example/pod/web-1')],
    },
  ],
};

function renderPage(path = '/projects/foo/namespaces/feat-1') {
  return render(
    <MemoryRouter initialEntries={[path]}>
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
    expect(headings).toEqual(['Ingresses', 'NodePort-Services', 'Business', 'Infrastruktur', 'Andere']);

    expect(screen.getByText('web.example')).toBeInTheDocument();
    expect(screen.getByText('web:80')).toBeInTheDocument();

    const nodePorts = screen.getByRole('heading', { name: 'NodePort-Services' }).parentElement!;
    expect(within(nodePorts).getByText('debug')).toBeInTheDocument();
    expect(within(nodePorts).getByText('80 → 30080')).toBeInTheDocument();

    const apps = screen.getByRole('heading', { name: 'Business' }).parentElement!;
    expect(within(apps).queryByText('debug')).not.toBeInTheDocument();
    // Port-Mappings der Anwendungs-Services werden nicht angezeigt.
    expect(within(apps).queryByText(/8080/)).not.toBeInTheDocument();
    expect(within(apps).getByRole('button', { name: 'Service neu starten' })).toBeInTheDocument();
    expect(within(apps).getByText('web-1')).toBeInTheDocument();
    expect(within(apps).getByText('Ready')).toBeInTheDocument();

    // Leere Bereiche bleiben sichtbar; ein Service kann in mehreren Bereichen stehen.
    const infra = screen.getByRole('heading', { name: 'Infrastruktur' }).parentElement!;
    expect(within(infra).getByText('Keine vorhanden')).toBeInTheDocument();
    const others = screen.getByRole('heading', { name: 'Andere' }).parentElement!;
    expect(within(others).getByText('web-1')).toBeInTheDocument();

    // Pod-Aktionen werden vorerst nicht angezeigt.
    expect(screen.queryByRole('button', { name: 'Pod löschen' })).not.toBeInTheDocument();
  });

  it('zeigt Aktionen nur als Icon mit dem Titel als Tooltip', async () => {
    mockFetch(() => ({ status: 200, body: data, etag: '"1"' }));
    renderPage();
    const button = await screen.findByRole('button', { name: 'Namespace löschen' });
    expect(button).not.toHaveTextContent('Namespace löschen');
    // Lucide-Icons werden dynamisch nachgeladen.
    await waitFor(() => expect(button.querySelector('svg')).not.toBeNull());

    await userEvent.setup().hover(button);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Namespace löschen');
  });

  it('filtert die Service-Bereiche per Quick-Filter mit Mehrfachauswahl', async () => {
    mockFetch(() => ({ status: 200, body: data, etag: '"1"' }));
    const user = userEvent.setup();
    renderPage();
    const areaHeadings = async () =>
      (await screen.findAllByRole('heading', { level: 2 })).map((h) => h.textContent).slice(2);

    expect(await areaHeadings()).toEqual(['Business', 'Infrastruktur', 'Andere']);
    const filter = screen.getByRole('group', { name: 'Service-Bereiche filtern' });
    expect(within(filter).getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'true');

    await user.click(within(filter).getByRole('button', { name: 'Business (1)' }));
    expect(await areaHeadings()).toEqual(['Business']);
    expect(within(filter).getByRole('button', { name: 'Alle' })).toHaveAttribute('aria-pressed', 'false');

    await user.click(within(filter).getByRole('button', { name: 'Andere (1)' }));
    expect(await areaHeadings()).toEqual(['Business', 'Andere']);

    await user.click(within(filter).getByRole('button', { name: 'Business (1)' }));
    expect(await areaHeadings()).toEqual(['Andere']);

    await user.click(within(filter).getByRole('button', { name: 'Alle' }));
    expect(await areaHeadings()).toEqual(['Business', 'Infrastruktur', 'Andere']);
  });

  it('übernimmt die Filter-Auswahl aus der URL und ignoriert unbekannte Bereiche', async () => {
    mockFetch(() => ({ status: 200, body: data, etag: '"1"' }));
    renderPage('/projects/foo/namespaces/feat-1?area=infra&area=gibt-es-nicht');
    await screen.findByRole('heading', { name: 'Infrastruktur' });
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(['Ingresses', 'NodePort-Services', 'Infrastruktur']);
  });

  it('zeigt keinen Filter bei nur einem Bereich', async () => {
    mockFetch(() => ({
      status: 200,
      body: { ...data, serviceAreas: [{ id: 'others', label: 'Anwendungs-Services', services: ['web'] }] },
      etag: '"1"',
    }));
    renderPage();
    await screen.findByRole('heading', { name: 'Anwendungs-Services' });
    expect(screen.queryByRole('group', { name: 'Service-Bereiche filtern' })).not.toBeInTheDocument();
  });

  it('zeigt Links an Namespace und Pods, die in einem neuen Tab öffnen', async () => {
    mockFetch(() => ({ status: 200, body: data, etag: '"1"' }));
    renderPage();
    const nsLink = await screen.findByRole('link', { name: 'Namespace-Logs' });
    expect(nsLink).toHaveAttribute('href', 'https://kibana.example/ns');
    expect(nsLink).toHaveAttribute('target', '_blank');
    expect(nsLink).toHaveAttribute('rel', 'noopener noreferrer');

    // Pod-Links stehen bei den Replicas, Pod-Aktionen weiterhin nicht.
    const podLinks = screen.getAllByRole('link', { name: 'Pod-Logs' });
    expect(podLinks.length).toBeGreaterThan(0);
    podLinks.forEach((l) => expect(l).toHaveAttribute('href', 'https://kibana.example/pod/web-1'));
    expect(screen.queryByRole('button', { name: 'Pod löschen' })).not.toBeInTheDocument();
  });

  it('meldet einen nicht (mehr) existierenden Namespace', async () => {
    mockFetch(() => ({ status: 404, body: { error: 'namespace nicht gefunden' } }));
    renderPage();
    expect(await screen.findByText(/existiert nicht \(mehr\)/)).toBeInTheDocument();
  });
});
