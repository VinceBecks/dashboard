import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockFetch } from '../test/fetchMock';
import { PollingSettingsProvider, usePollingSettings } from './PollingSettings';
import { usePolling } from './usePolling';

afterEach(() => vi.unstubAllGlobals());

describe('usePolling', () => {
  it('sendet den ETag und behält die Daten bei 304', async () => {
    let calls = 0;
    const fetchMock = mockFetch(() => {
      calls++;
      return calls === 1 ? { status: 200, body: { v: 1 }, etag: '"1"' } : { status: 304 };
    });

    const { result } = renderHook(() => usePolling<{ v: number }>('/api/x', 20));
    await waitFor(() => expect(result.current.data).toEqual({ v: 1 }));
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2));

    const headers = fetchMock.mock.calls[1][1]?.headers as Record<string, string>;
    expect(headers['If-None-Match']).toBe('"1"');
    expect(result.current.data).toEqual({ v: 1 });
    expect(result.current.error).toBeUndefined();
  });

  it('ersetzt die Daten bei 200 und meldet Fehler ohne die Daten zu verwerfen', async () => {
    const responses = [
      { status: 200, body: { v: 1 }, etag: '"1"' },
      { status: 500, body: { error: 'kaputt' } },
      { status: 200, body: { v: 2 }, etag: '"2"' },
    ];
    let i = 0;
    mockFetch(() => responses[Math.min(i++, responses.length - 1)]);

    const { result } = renderHook(() => usePolling<{ v: number }>('/api/x', 1_000_000));
    await waitFor(() => expect(result.current.data).toEqual({ v: 1 }));

    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.error?.message).toBe('kaputt'));
    expect(result.current.data).toEqual({ v: 1 });

    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.data).toEqual({ v: 2 }));
    expect(result.current.error).toBeUndefined();
  });

  it('fragt beim Intervall 0 nicht automatisch ab, sondern nur bei refreshAll()', async () => {
    let calls = 0;
    mockFetch((url) => {
      if (url === '/api/polling') {
        return { status: 200, body: { intervals: [{ value: '0s', milliseconds: 0 }], defaultInterval: '0s', manualRefreshCooldownMilliseconds: 1000 } };
      }
      calls++;
      return { status: 200, body: { v: calls } };
    });
    const wrapper = ({ children }: { children: ReactNode }) => <PollingSettingsProvider>{children}</PollingSettingsProvider>;

    const { result } = renderHook(() => ({ poll: usePolling<{ v: number }>('/api/x'), settings: usePollingSettings() }), { wrapper });
    await waitFor(() => expect(result.current.poll.data).toEqual({ v: 1 }));
    await new Promise((r) => setTimeout(r, 50));
    expect(calls).toBe(1);

    act(() => result.current.settings.refreshAll());
    await waitFor(() => expect(result.current.poll.data).toEqual({ v: 2 }));
  });
});
