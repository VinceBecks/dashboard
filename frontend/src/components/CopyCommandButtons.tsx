import { Box, IconButton, Stack, Tooltip } from '@mui/material';
import { useEffect, useState } from 'react';
import type { CopyCommand } from '../api/types';
import { ActionIcon } from './ActionIcon';
import { copyToClipboard } from './CopyButton';

/**
 * Icon-Buttons, die einen vom Backend fertig gerenderten Befehl (z. B. kubectl exec) in die Zwischenablage kopieren.
 * Der Tooltip zeigt Titel und Befehl.
 */
export function CopyCommandButtons({ commands }: { commands: CopyCommand[] }) {
  if (commands.length === 0) {
    return null;
  }
  return (
    <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'nowrap' }} role="group" aria-label="Befehle kopieren">
      {commands.map((c) => (
        <CopyCommandButton key={c.id} command={c} />
      ))}
    </Stack>
  );
}

function CopyCommandButton({ command }: { command: CopyCommand }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');

  useEffect(() => {
    if (state === 'idle') {
      return;
    }
    const timer = setTimeout(() => setState('idle'), 2000);
    return () => clearTimeout(timer);
  }, [state]);

  const title =
    state === 'copied' ? (
      'Kopiert'
    ) : state === 'failed' ? (
      'Kopieren nicht möglich'
    ) : (
      <>
        {command.title}
        <Box component="code" sx={{ display: 'block', mt: 0.5, fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
          {command.command}
        </Box>
      </>
    );
  return (
    <Tooltip title={title}>
      <IconButton
        size="small"
        aria-label={`${command.title} kopieren`}
        onClick={() =>
          copyToClipboard(command.command).then(
            () => setState('copied'),
            () => setState('failed'),
          )
        }
      >
        <ActionIcon name={command.lucideIcon} color={command.lucideIconColor} size={20} />
      </IconButton>
    </Tooltip>
  );
}
