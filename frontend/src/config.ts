/** Abfrage-Intervall für das Backend. Dank ETag/304 sind unveränderte Abfragen günstig. */
export const POLL_INTERVAL_MS = 10_000;

/** Cluster-Angaben (z. B. DNS-Name der Nodes) ändern sich nur mit einem Neustart des Backends. */
export const CLUSTER_POLL_INTERVAL_MS = 300_000;
