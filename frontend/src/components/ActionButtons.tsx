import { Alert, Button, Snackbar, Stack } from '@mui/material';
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

/** Buttons für alle Aktionen eines Objekts – vollständig aus den Backend-Daten aufgebaut. */
export function ActionButtons({ actions, object, onExecuted }: Props) {
  const [active, setActive] = useState<Action>();
  const [result, setResult] = useState<ActionResult>();

  if (actions.length === 0) {
    return null;
  }

  return (
    <>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
        {actions.map((a) => (
          <Button
            key={a.id}
            size="small"
            variant="outlined"
            startIcon={<ActionIcon name={a.lucideIcon} color={a.lucideIconColor} />}
            onClick={() => setActive(a)}
          >
            {a.title}
          </Button>
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
