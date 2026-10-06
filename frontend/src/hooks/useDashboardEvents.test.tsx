import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { service, snapshot } from '../test/fixtures';
import { EVENTS_URL, useDashboardEvents } from './useDashboardEvents';

class FakeEventSource {
  static last?: FakeEventSource;
  readonly listeners = new Map<string, (e: MessageEvent) => void>();
  onerror: (() => void) | null = null;
  closed = false;

  constructor(readonly url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, fn);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data: unknown) {
    act(() => this.listeners.get(type)!(new MessageEvent(type, { data: JSON.stringify(data) })));
  }
}

afterEach(() => vi.unstubAllGlobals());

describe('useDashboardEvents', () => {
  it('lädt den Snapshot und wendet danach Änderungen an', () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    const { result, unmount } = renderHook(() => useDashboardEvents());
    const source = FakeEventSource.last!;
    expect(source.url).toBe(EVENTS_URL);
    expect(result.current).toEqual({ connection: 'connecting' });

    source.emit('snapshot', snapshot());
    expect(result.current.connection).toBe('open');

    source.emit('upsert', { ...service('worker', true), pods: undefined });
    expect(result.current.data!.projects[0].namespaces[0].services![1].running).toBe(true);

    source.emit('delete', { kind: 'namespace', name: 'feat-1', parents: { project: 'foo' } });
    expect(result.current.data!.projects[0].namespaces).toEqual([]);

    act(() => source.onerror!());
    expect(result.current.connection).toBe('reconnecting');

    unmount();
    expect(source.closed).toBe(true);
  });
});
