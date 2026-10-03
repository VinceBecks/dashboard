/**
 * Abfrage-Vorgaben, falls GET /api/polling nicht geladen werden kann (und außerhalb des PollingSettingsProvider,
 * z. B. in Tests). Normalerweise kommen Intervalle, Default und Sperrzeit aus dem Backend.
 */
export const FALLBACK_POLL_INTERVAL_MS = 10_000;
export const FALLBACK_MANUAL_REFRESH_COOLDOWN_MS = 3_000;

/** Cluster-Angaben (z. B. DNS-Name der Nodes) ändern sich nur mit einem Neustart des Backends. */
export const CLUSTER_POLL_INTERVAL_MS = 300_000;
