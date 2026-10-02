import { Alert, IconButton, Snackbar, Stack, Tooltip } from '@mui/material';
import { useState } from 'react';
import type { Action, ActionResult, ObjectRef } from '../api/types';
import { ActionDialog } from './ActionDialog';
import { ActionIcon } from './ActionIcon';

interface Props {
  actions: Action[];
  object: ObjectRef;
  /** Wird nach einer ausgeführten Aktion aufgerufen, um die Daten sofort neu zu laden. */
  onExecuted?: () => void;
}

/** Icon-Buttons für alle Aktionen eines Objekts – vollständig aus den Backend-Daten aufgebaut. */
export function ActionButtons({ actions, object, onExecuted }: Props) {
  const [active, setActive] = useState<Action>();
  const [result, setResult] = useState<ActionResult>();

  if (actions.length === 0) {
    return null;
  }

  return (
    <>
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }} role="group" aria-label="Aktionen">
        {actions.map((a) => (
          // Nur das Icon; der Titel erscheint als Tooltip und dient als zugänglicher Name.
          <Tooltip key={a.id} title={a.title}>
            <IconButton size="small" aria-label={a.title} onClick={() => setActive(a)}>
              <ActionIcon name={a.lucideIcon} color={a.lucideIconColor} size={20} />
            </IconButton>
          </Tooltip>
        ))}
      </Stack>
      {active && (
        <ActionDialog
          action={active}
          object={object}
          onClose={() => setActive(undefined)}
          onResult={(r) => {
            setActive(undefined);
            setResult(r);
            onExecuted?.();
          }}
        />
      )}
      <Snackbar
        open={result !== undefined}
        autoHideDuration={6000}
        onClose={() => setResult(undefined)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={result?.success ? 'success' : 'error'} onClose={() => setResult(undefined)} variant="filled">
          {result?.message}
        </Alert>
      </Snackbar>
    </>
  );
}
