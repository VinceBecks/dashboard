import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { namespace, nodePortService, pod, service } from '../test/fixtures';
import { NamespaceCard } from './NamespaceCard';

describe('NamespaceCard', () => {
  it('zeigt Ingress-URLs und je Service einen grünen oder roten Punkt', () => {
    render(<NamespaceCard namespace={namespace([service('web', true), service('worker', false)])} />);
    expect(screen.getByRole('link', { name: 'https://feat-1.example/api' })).toHaveAttribute('href', 'https://feat-1.example/api');
    const web = screen.getByRole('listitem', { name: 'web: läuft' });
    const worker = screen.getByRole('listitem', { name: 'worker: läuft nicht' });
    expect(web.querySelector('[data-running="true"]')).not.toBeNull();
    expect(worker.querySelector('[data-running="false"]')).not.toBeNull();
  });

  it('öffnet per Klick auf einen Punkt die Service-Details mit Replicas und deren Aktionen', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<NamespaceCard namespace={namespace([service('web', true, [pod('web-1', true)])])} />);
    await user.click(screen.getByRole('listitem', { name: 'web: läuft' }));

    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByText('web-1')).toBeInTheDocument();
    expect(within(dialog).getByText('Ready')).toBeInTheDocument();
    expect(within(dialog).getByText('app: registry/app:1')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Pod löschen' })).toBeInTheDocument();

    // Der Dialog folgt dem Event-Stream: Service verschwindet → Hinweis.
    rerender(<NamespaceCard namespace={namespace([])} />);
    expect(within(screen.getByRole('dialog')).getByText('Der Service existiert nicht mehr.')).toBeInTheDocument();
  });

  it('zeigt NodePort-Services mit kopierbaren Adressen und ihren Aktionen', () => {
    render(<NamespaceCard namespace={namespace([], [nodePortService('web-node-port', ['nodes.example:30080', 'nodes.example:30081'])])} />);
    expect(screen.getByText('web-node-port')).toBeInTheDocument();
    expect(screen.getByText('nodes.example:30080')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'nodes.example:30081 kopieren' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'NodePort-Service löschen' })).toBeInTheDocument();
  });

  it('zeigt einen Hinweis ohne Services', () => {
    render(<NamespaceCard namespace={namespace([])} />);
    expect(screen.getByText('Keine Anwendungs-Services')).toBeInTheDocument();
  });
});
