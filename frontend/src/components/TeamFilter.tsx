import { Chip, Stack } from '@mui/material';
import type { Team } from '../api/types';
import { NO_TEAM } from './teams';

interface Props {
  teams: Team[];
  selected: string[];
  onChange: (selected: string[]) => void;
}

/** Quick-Filter: je Team ein Chip plus „Ohne Team“, mehrere gleichzeitig wählbar. Ohne Auswahl wird alles gezeigt. */
export function TeamFilter({ teams, selected, onChange }: Props) {
  const options = [...teams, { id: NO_TEAM, label: 'Ohne Team' }];
  const toggle = (id: string) => onChange(selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id]);
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mb: 2 }} role="group" aria-label="Team-Filter">
      {options.map((t) => {
        const active = selected.includes(t.id);
        return (
          <Chip
            key={t.id}
            label={t.label}
            size="small"
            clickable
            color={active ? 'primary' : 'default'}
            variant={active ? 'filled' : 'outlined'}
            aria-pressed={active}
            onClick={() => toggle(t.id)}
          />
        );
      })}
    </Stack>
  );
}
