import { Chip } from '@mui/material';
import type { PodObject } from '../api/types';

export function PodStatus({ pod }: { pod: PodObject }) {
  return <Chip size="small" color={pod.ready ? 'success' : 'warning'} label={pod.ready ? 'Ready' : 'Nicht ready'} />;
}
