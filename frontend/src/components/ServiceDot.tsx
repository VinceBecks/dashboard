import { Box, keyframes } from '@mui/material';
import type { ServiceObject } from '../api/types';

const pulse = keyframes`0% { box-shadow: 0 0 0 0 rgba(2, 136, 209, 0.7); } 70% { box-shadow: 0 0 0 6px rgba(2, 136, 209, 0); } 100% { box-shadow: 0 0 0 0 rgba(2, 136, 209, 0); }`;

/** Läuft eine Pipeline am Service oder an einem seiner Replicas? */
export function hasRunningPipeline(service: ServiceObject): boolean {
  return service.pipelines.length > 0 || (service.pods ?? []).some((p) => p.pipelines.length > 0);
}

/**
 * Grüner Punkt, wenn mindestens ein Replica läuft, sonst rot. Läuft am Service oder an einem Replica eine Pipeline,
 * pulsiert ein Rand um den Punkt.
 */
export function ServiceDot({ service, size = 14 }: { service: ServiceObject; size?: number }) {
  const pipeline = hasRunningPipeline(service);
  return (
    <Box
      component="span"
      data-running={service.running}
      data-pipeline={pipeline}
      sx={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: '50%',
        flexShrink: 0,
        bgcolor: service.running ? 'success.main' : 'error.main',
        ...(pipeline && { outline: '2px solid', outlineColor: 'info.main', outlineOffset: 1, animation: `${pulse} 1.6s infinite` }),
      }}
    />
  );
}
