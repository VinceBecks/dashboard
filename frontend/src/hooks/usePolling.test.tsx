import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockFetch } from '../test/fetchMock';
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
});
