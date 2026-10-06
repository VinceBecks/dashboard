import { Box } from '@mui/material';
import type { ServiceObject } from '../api/types';

/** Grüner Punkt, wenn mindestens ein Replica läuft, sonst rot. */
export function ServiceDot({ service, size = 14 }: { service: ServiceObject; size?: number }) {
  return (
    <Box
      component="span"
      data-running={service.running}
      sx={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: '50%',
        flexShrink: 0,
        bgcolor: service.running ? 'success.main' : 'error.main',
      }}
    />
  );
}
