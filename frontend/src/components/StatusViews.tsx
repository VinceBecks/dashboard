import { Alert, Box, CircularProgress } from '@mui/material';

export function Loading() {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
      <CircularProgress aria-label="Lädt" />
    </Box>
  );
}

/** Fehler beim Abfragen. Vorhandene Daten bleiben darunter sichtbar. */
export function PollError({ error }: { error: Error }) {
  return (
    <Alert severity="warning" sx={{ mb: 2 }}>
      Daten konnten nicht aktualisiert werden: {error.message}
    </Alert>
  );
}
