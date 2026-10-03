import { keyframes } from '@emotion/react';
import { Box, FormControl, IconButton, InputLabel, MenuItem, Select, Tooltip } from '@mui/material';
import { RefreshCw } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { usePollingSettings } from '../hooks/PollingSettings';

const spin = keyframes`from { transform: rotate(0deg); } to { transform: rotate(360deg); }`;

/**
 * Auswahl des Abfrage-Intervalls. Beim Intervall 0 („Manuell“) gibt es stattdessen einen Aktualisieren-Button, der
 * nach einem Klick für die Sperrzeit aus dem Backend gesperrt ist.
 */
export function PollingControl() {
  const { intervals, selected, intervalMs, select, cooldownMs, refreshAll } = usePollingSettings();
  const [coolingDown, setCoolingDown] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  const refresh = () => {
    if (coolingDown) {
      return;
    }
    refreshAll();
    setCoolingDown(true);
    timer.current = setTimeout(() => setCoolingDown(false), cooldownMs);
  };

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      {intervalMs === 0 && (
        <Tooltip title="Aktualisieren">
          <span>
            <IconButton color="inherit" aria-label="Aktualisieren" onClick={refresh} disabled={coolingDown}>
              <Box component={RefreshCw} sx={{ animation: coolingDown ? `${spin} 1s linear infinite` : undefined }} />
            </IconButton>
          </span>
        </Tooltip>
      )}
      <FormControl size="small" sx={{ minWidth: 140 }}>
        <InputLabel id="polling-interval-label" sx={{ color: 'inherit' }}>
          Aktualisierung
        </InputLabel>
        <Select
          labelId="polling-interval-label"
          label="Aktualisierung"
          value={selected}
          onChange={(e) => select(e.target.value)}
          sx={{ color: 'inherit', '.MuiOutlinedInput-notchedOutline': { borderColor: 'currentColor' }, '.MuiSvgIcon-root': { color: 'inherit' } }}
        >
          {intervals.map((i) => (
            <MenuItem key={i.value} value={i.value}>
              {i.milliseconds === 0 ? 'Manuell' : `alle ${i.value}`}
            </MenuItem>
          ))}
        </Select>
      </FormControl>
    </Box>
  );
}
