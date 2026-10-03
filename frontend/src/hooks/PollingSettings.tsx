import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { getConditional } from '../api/client';
import type { PollingInterval, PollingResponse } from '../api/types';
import { Loading } from '../components/StatusViews';
import { FALLBACK_MANUAL_REFRESH_COOLDOWN_MS, FALLBACK_POLL_INTERVAL_MS } from '../config';

export interface PollingSettings {
  /** Wählbare Intervalle aus dem Backend. */
  intervals: PollingInterval[];
  /** value des gewählten Intervalls. */
  selected: string;
  /** Gewähltes Intervall in Millisekunden; 0 heißt: nur manuell aktualisieren. */
  intervalMs: number;
  select: (value: string) => void;
  /** Sperrzeit des Aktualisieren-Buttons nach einem Klick. */
  cooldownMs: number;
  /** Zähler, der bei jedem refreshAll() steigt; usePolling fragt dann neu ab. */
  refreshToken: number;
  refreshAll: () => void;
  /** Fehler beim Laden von GET /api/polling; dann gelten die Fallback-Werte. */
  error?: Error;
}

const fallbackResponse: PollingResponse = {
  intervals: [{ value: `${FALLBACK_POLL_INTERVAL_MS / 1000}s`, milliseconds: FALLBACK_POLL_INTERVAL_MS }],
  defaultInterval: `${FALLBACK_POLL_INTERVAL_MS / 1000}s`,
  manualRefreshCooldownMilliseconds: FALLBACK_MANUAL_REFRESH_COOLDOWN_MS,
};

const fallback: PollingSettings = {
  intervals: fallbackResponse.intervals,
  selected: fallbackResponse.defaultInterval,
  intervalMs: FALLBACK_POLL_INTERVAL_MS,
  select: () => {},
  cooldownMs: FALLBACK_MANUAL_REFRESH_COOLDOWN_MS,
  refreshToken: 0,
  refreshAll: () => {},
};

const PollingSettingsContext = createContext<PollingSettings>(fallback);

/** Abfrage-Einstellungen der Seite; außerhalb des Providers gelten die Fallback-Werte aus config.ts. */
export function usePollingSettings(): PollingSettings {
  return useContext(PollingSettingsContext);
}

/**
 * Lädt die Abfrage-Vorgaben einmal aus GET /api/polling und hält das gewählte Intervall. Die Auswahl wird nicht
 * gespeichert; nach dem Neuladen gilt wieder der Default aus dem Backend.
 */
export function PollingSettingsProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<{ response: PollingResponse; error?: Error }>();
  const [selected, setSelected] = useState<string>();
  const [refreshToken, setRefreshToken] = useState(0);
  const refreshAll = useCallback(() => setRefreshToken((t) => t + 1), []);

  useEffect(() => {
    const controller = new AbortController();
    getConditional<PollingResponse>('/api/polling', null, controller.signal)
      .then((res) => {
        if (!res.notModified) {
          setConfig({ response: res.data });
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) {
          setConfig({ response: fallbackResponse, error: e as Error });
        }
      });
    return () => controller.abort();
  }, []);

  const value = useMemo<PollingSettings | undefined>(() => {
    if (!config) {
      return undefined;
    }
    const { intervals, defaultInterval, manualRefreshCooldownMilliseconds } = config.response;
    const current =
      intervals.find((i) => i.value === (selected ?? defaultInterval)) ??
      intervals.find((i) => i.value === defaultInterval) ??
      intervals[0];
    return {
      intervals,
      selected: current.value,
      intervalMs: current.milliseconds,
      select: setSelected,
      cooldownMs: manualRefreshCooldownMilliseconds,
      refreshToken,
      refreshAll,
      error: config.error,
    };
  }, [config, selected, refreshToken, refreshAll]);

  if (!value) {
    return <Loading />;
  }
  return <PollingSettingsContext.Provider value={value}>{children}</PollingSettingsContext.Provider>;
}
