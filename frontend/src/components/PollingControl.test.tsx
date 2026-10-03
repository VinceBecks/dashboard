import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PollingResponse } from '../api/types';
import { PollingSettingsProvider } from '../hooks/PollingSettings';
import { usePolling } from '../hooks/usePolling';
import { mockFetch } from '../test/fetchMock';
import { PollingControl } from './PollingControl';

afterEach(() => vi.unstubAllGlobals());

const polling: PollingResponse = {
  intervals: [
    { value: '0s', milliseconds: 0 },
    { value: '10s', milliseconds: 10_000 },
  ],
  defaultInterval: '10s',
  manualRefreshCooldownMilliseconds: 300,
};

function Consumer() {
  const { data } = usePolling<{ v: number }>('/api/x');
  return <div>Daten {data?.v}</div>;
}

function setup(response: PollingResponse = polling) {
  let calls = 0;
  mockFetch((url) => {
    if (url === '/api/polling') {
      return { status: 200, body: response };
    }
    calls++;
    return { status: 200, body: { v: calls } };
  });
  render(
    <PollingSettingsProvider>
      <PollingControl />
      <Consumer />
    </PollingSettingsProvider>,
  );
  return { calls: () => calls };
}

describe('PollingControl', () => {
  it('zeigt die Intervalle aus dem Backend mit dem Default', async () => {
    setup();
    const select = await screen.findByRole('combobox', { name: 'Aktualisierung' });
    expect(select).toHaveTextContent('alle 10s');
    expect(screen.queryByRole('button', { name: 'Aktualisieren' })).not.toBeInTheDocument();

    await userEvent.click(select);
    const options = within(screen.getByRole('listbox')).getAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual(['Manuell', 'alle 10s']);
  });

  it('fragt bei „Manuell“ nur per Button ab und sperrt ihn für die Sperrzeit', async () => {
    const { calls } = setup({ ...polling, defaultInterval: '0s' });
    await screen.findByText('Daten 1');

    const button = screen.getByRole('button', { name: 'Aktualisieren' });
    await userEvent.click(button);
    await screen.findByText('Daten 2');
    expect(button).toBeDisabled();

    // Gesperrt: weitere Klicks lösen keine Abfrage aus.
    await userEvent.click(button, { pointerEventsCheck: 0 });
    expect(calls()).toBe(2);

    await waitFor(() => expect(button).toBeEnabled());
    await userEvent.click(button);
    await screen.findByText('Daten 3');
  });
});
