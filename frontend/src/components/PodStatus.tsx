import { Chip } from '@mui/material';
import type { PodObject } from '../api/types';

export function PodStatus({ pod }: { pod: PodObject }) {
  const color = pod.ready ? 'success' : pod.phase === 'Failed' ? 'error' : 'warning';
  return <Chip size="small" color={color} label={pod.ready ? 'Ready' : pod.phase || 'Unbekannt'} />;
}
