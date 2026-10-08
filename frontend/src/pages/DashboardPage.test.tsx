import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';
import type { NamespaceObject, Snapshot } from '../api/types';
import { filterByTeams } from '../components/teams';
import { namespace, snapshot } from '../test/fixtures';
import { DashboardPage } from './DashboardPage';

function ns(name: string, team?: string): NamespaceObject {
  return { ...namespace([]), name, team };
}

function data(): Snapshot {
  const s = snapshot();
  s.projects[0].namespaces = [ns('feat-a', 'checkout'), ns('feat-b', 'search'), ns('feat-c')];
  return s;
}

afterEach(() => window.history.replaceState(null, '', '/'));

const cards = () => screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent);

describe('Team-Filter', () => {
  it('filtert nach Teams und „Ohne Team“', () => {
    const all = data().projects[0].namespaces;
    expect(filterByTeams(all, []).map((n) => n.name)).toEqual(['feat-a', 'feat-b', 'feat-c']);
    expect(filterByTeams(all, ['search']).map((n) => n.name)).toEqual(['feat-b']);
    expect(filterByTeams(all, ['checkout', '-']).map((n) => n.name)).toEqual(['feat-a', 'feat-c']);
  });

  it('zeigt je Projekt Chips, filtert per Mehrfachauswahl und merkt sich die Auswahl in der URL', async () => {
    const user = userEvent.setup();
    render(<DashboardPage data={data()} />);
    const foo = screen.getByRole('region', { name: 'foo' });
    const filter = within(foo).getByRole('group', { name: 'Team-Filter' });
    expect(within(filter).getAllByRole('button').map((b) => b.textContent)).toEqual(['Team Checkout', 'Team Suche', 'Ohne Team']);
    expect(cards()).toEqual(['feat-a', 'feat-b', 'feat-c']);

    await user.click(within(filter).getByRole('button', { name: 'Team Suche' }));
    expect(cards()).toEqual(['feat-b']);
    await user.click(within(filter).getByRole('button', { name: 'Ohne Team' }));
    expect(cards()).toEqual(['feat-b', 'feat-c']);
    expect(new URLSearchParams(window.location.search).getAll('team.foo')).toEqual(['search', '-']);

    // Projekt ohne Namespaces zeigt keinen Filter.
    expect(within(screen.getByRole('region', { name: 'bar' })).queryByRole('group', { name: 'Team-Filter' })).toBeNull();
  });

  it('übernimmt die Auswahl aus der URL und zeigt das Team an der Kachel', () => {
    window.history.replaceState(null, '', '/?team.foo=checkout');
    render(<DashboardPage data={data()} />);
    expect(cards()).toEqual(['feat-a']);
    expect(screen.getByLabelText('Team: Team Checkout')).toBeInTheDocument();
  });

  it('zeigt einen Hinweis, wenn kein Namespace zur Auswahl passt, und ohne Teams keinen Filter', () => {
    window.history.replaceState(null, '', '/?team.foo=gibts-nicht');
    const { unmount } = render(<DashboardPage data={data()} />);
    expect(screen.getByText('Keine Feature-Umgebungen für die gewählten Teams')).toBeInTheDocument();
    unmount();
    render(<DashboardPage data={{ ...data(), teams: [] }} />);
    expect(screen.queryByRole('group', { name: 'Team-Filter' })).toBeNull();
  });
});
