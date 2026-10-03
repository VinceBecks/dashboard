import { useCallback, useEffect, useRef, useState } from 'react';
import { getConditional } from '../api/client';
import { usePollingSettings } from './PollingSettings';

export interface PollingState<T> {
  data: T | undefined;
  error: Error | undefined;
  loading: boolean;
  /** Sofort neu abfragen, z. B. nach einer Aktion. */
  refresh: () => void;
}

/**
 * Fragt url regelmäßig ab. Bei 304 bleibt der Zustand unverändert, bei 200 wird er ersetzt.
 * Wechselt die url, beginnt die Abfrage ohne ETag und ohne alte Daten.
 *
 * Ohne intervalMs gilt das im Dropdown gewählte Intervall (PollingSettings); dann fragt der Hook auch bei
 * refreshAll() neu ab. Ein Intervall von 0 heißt: nicht automatisch abfragen.
 */
export function usePolling<T>(url: string, intervalMs?: number): PollingState<T> {
  const settings = usePollingSettings();
  const effectiveIntervalMs = intervalMs ?? settings.intervalMs;
  const refreshToken = intervalMs === undefined ? settings.refreshToken : 0;
  const [state, setState] = useState<{ url: string; data?: T; error?: Error }>({ url });
  const etag = useRef<string | null>(null);
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((t) => t + 1), []);

  if (state.url !== url) {
    // Neue Seite/Objekt: alten Zustand verwerfen (Muster „Zustand beim Rendern anpassen“).
    setState({ url });
  }

  useEffect(() => {
    etag.current = null;
  }, [url]);

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      try {
        const res = await getConditional<T>(url, etag.current, controller.signal);
        if (!res.notModified) {
          etag.current = res.etag;
          setState({ url, data: res.data });
        } else {
          setState((s) => (s.error ? { url, data: s.data } : s));
        }
      } catch (e) {
        if (controller.signal.aborted) {
          return;
        }
        etag.current = null;
        setState((s) => ({ url, data: s.url === url ? s.data : undefined, error: e as Error }));
      }
      if (!controller.signal.aborted && effectiveIntervalMs > 0) {
        timer = setTimeout(poll, effectiveIntervalMs);
      }
    };
    void poll();

    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [url, effectiveIntervalMs, tick, refreshToken]);

  const current = state.url === url ? state : { data: undefined, error: undefined };
  return {
    data: current.data,
    error: current.error,
    loading: current.data === undefined && current.error === undefined,
    refresh,
  };
}
