import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Action } from '../api/types';
import { mockFetch } from '../test/fetchMock';
import { ActionDialog } from './ActionDialog';

afterEach(() => vi.unstubAllGlobals());

const action: Action = {
  id: 'scale',
  title: 'Skalieren',
  lucideIcon: 'scale',
  lucideIconColor: 'blue',
  formFields: [
    { id: 'replicas', label: 'Anzahl', type: 'number', isRequired: true },
    { id: 'reason', label: 'Grund', type: 'text', isRequired: false, description: 'optional' },
    {
      id: 'mode',
      label: 'Modus',
      type: 'select',
      isRequired: true,
      options: [
        { value: 'fast', label: 'Schnell' },
        { value: 'safe', label: 'Sicher' },
      ],
    },
  ],
};

const object = { kind: 'service' as const, namespace: 'feat-1', name: 'web' };

describe('ActionDialog', () => {
  it('baut das Formular aus den Feldern und sendet typisierte Eingaben', async () => {
    const fetchMock = mockFetch(() => ({ status: 200, body: { success: true, message: 'ok' } }));
    const onResult = vi.fn();
    const user = userEvent.setup();
    render(<ActionDialog action={action} object={object} onClose={() => {}} onResult={onResult} />);

    await user.click(screen.getByRole('button', { name: 'Ausführen' }));
    expect(screen.getAllByText('Pflichtfeld')).toHaveLength(2);
    expect(fetchMock).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText(/Anzahl/), '3');
    await user.click(screen.getByRole('combobox'));
    await user.click(within(screen.getByRole('listbox')).getByText('Sicher'));
    await user.click(screen.getByRole('button', { name: 'Ausführen' }));

    await waitFor(() => expect(onResult).toHaveBeenCalledWith({ success: true, message: 'ok' }));
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/actions/scale');
    expect(init?.method).toBe('PATCH');
    expect(JSON.parse(init?.body as string)).toEqual({ object, inputs: { replicas: 3, mode: 'safe' } });
  });

  it('zeigt Validierungsfehler des Backends an', async () => {
    mockFetch(() => ({ status: 400, body: { error: 'ungültige Eingaben', problems: ['replicas: zu groß'] } }));
    const user = userEvent.setup();
    render(
      <ActionDialog
        action={{ ...action, formFields: [] }}
        object={object}
        onClose={() => {}}
        onResult={() => {}}
      />,
    );
    await user.click(screen.getByRole('button', { name: 'Ausführen' }));
    expect(await screen.findByText('replicas: zu groß')).toBeInTheDocument();
  });
});
