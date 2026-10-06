import { Chip } from '@mui/material';
import type { ConnectionState } from '../hooks/useDashboardEvents';

const labels: Record<ConnectionState, { label: string; color: 'success' | 'default' | 'warning' }> = {
  open: { label: 'Live', color: 'success' },
  connecting: { label: 'Verbinde…', color: 'default' },
  reconnecting: { label: 'Getrennt – verbinde neu…', color: 'warning' },
};

/** Zustand der Event-Verbindung zum Backend (oben rechts). */
export function ConnectionStatus({ state }: { state: ConnectionState }) {
  const { label, color } = labels[state];
  return <Chip size="small" color={color} label={label} variant={state === 'open' ? 'filled' : 'outlined'} />;
}
