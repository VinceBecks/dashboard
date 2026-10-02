import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Pod, Service } from '../api/types';
import { mockFetch } from '../test/fetchMock';
import { ServiceCard } from './ServiceCard';

afterEach(() => vi.unstubAllGlobals());

const service: Service = {
  name: 'web',
  type: 'ClusterIP',
  ports: [],
  replicas: ['web-1'],
  actions: [{ id: 'restart', title: 'Neustart', lucideIcon: 'rotate-ccw', lucideIconColor: 'orange' }],
  links: [],
};

const pod: Pod = {
  name: 'web-1',
  phase: 'Running',
  ready: true,
  containers: [{ name: 'nginx', image: 'nginx:1.27', ports: [{ name: 'http', containerPort: 80, protocol: 'TCP' }] }],
  actions: [{ id: 'delete-pod', title: 'Pod löschen', lucideIcon: 'trash', lucideIconColor: 'red' }],
  links: [{ id: 'logs', title: 'Pod-Logs', lucideIcon: 'scroll-text', lucideIconColor: 'teal', url: 'https://kibana.example/pod' }],
};

function renderCard(pods: Pod[] = [pod], onExecuted = () => {}) {
  return render(<ServiceCard service={service} pods={pods} namespace="feat-1" onExecuted={onExecuted} />);
}

describe('ServiceCard', () => {
  it('dreht sich beim Klick auf ein Replica um und zeigt Infos, Aktionen und Links des Pods', async () => {
    const user = userEvent.setup();
    renderCard();
    const card = screen.getByTestId('service-card-web');
    expect(card).toHaveAttribute('data-flipped', 'false');
    // Vorderseite: keine Pod-Aktionen.
    expect(screen.queryByRole('button', { name: 'Pod löschen' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Details zu Pod web-1' }));

    expect(card).toHaveAttribute('data-flipped', 'true');
    const back = within(screen.getByRole('heading', { name: 'web-1' }).closest<HTMLElement>('.MuiCard-root')!);
    expect(back.getByText('Pod')).toBeInTheDocument();
    expect(back.getByText('Ready')).toBeInTheDocument();
    expect(back.getByText('nginx')).toBeInTheDocument();
    expect(back.getByText('nginx:1.27')).toBeInTheDocument();
    expect(back.getByText('http: 80')).toBeInTheDocument();
    expect(within(screen.getByRole('group', { name: 'Aktionen' })).getByRole('button', { name: 'Pod löschen' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Pod-Logs' })).toHaveAttribute('href', 'https://kibana.example/pod');
    // Die Vorderseite ist verdeckt und für Screenreader ausgeblendet.
    expect(screen.queryByRole('button', { name: 'Neustart' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Zurück zum Service' }));
    expect(card).toHaveAttribute('data-flipped', 'false');
    expect(screen.getByRole('button', { name: 'Neustart' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Pod löschen' })).not.toBeInTheDocument();
  });

  it('führt Pod-Aktionen auf der Rückseite aus', async () => {
    const fetchMock = mockFetch(() => ({ status: 200, body: { success: true, message: 'Pod wird gelöscht' } }));
    const onExecuted = vi.fn();
    const user = userEvent.setup();
    renderCard([pod], onExecuted);

    await user.click(screen.getByRole('button', { name: 'Details zu Pod web-1' }));
    await user.click(screen.getByRole('button', { name: 'Pod löschen' }));
    await user.click(screen.getByRole('button', { name: 'Ausführen' }));

    await waitFor(() => expect(onExecuted).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/actions/delete-pod');
    expect(JSON.parse(init?.body as string).object).toEqual({ kind: 'pod', namespace: 'feat-1', name: 'web-1' });
  });

  it('meldet einen inzwischen gelöschten Pod auf der Rückseite', async () => {
    const user = userEvent.setup();
    const { rerender } = renderCard();
    await user.click(screen.getByRole('button', { name: 'Details zu Pod web-1' }));

    rerender(<ServiceCard service={{ ...service, replicas: [] }} pods={[]} namespace="feat-1" onExecuted={() => {}} />);
    expect(screen.getByText('Der Pod existiert nicht mehr.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Zurück zum Service' }));
    expect(screen.getByText('Keine Replicas')).toBeInTheDocument();
  });
});
