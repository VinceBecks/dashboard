import { Chip, Stack } from '@mui/material';
import type { ServiceArea } from '../api/types';

interface Props {
  areas: ServiceArea[];
  /** IDs der ausgewählten Bereiche; leer bedeutet: alle anzeigen. */
  selected: string[];
  onChange: (selected: string[]) => void;
}

/** Quick-Filter über den Service-Bereichen. Mehrere Bereiche können gleichzeitig ausgewählt werden. */
export function ServiceAreaFilter({ areas, selected, onChange }: Props) {
  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id]);

  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }} role="group" aria-label="Service-Bereiche filtern">
      <Chip
        label="Alle"
        color={selected.length === 0 ? 'primary' : 'default'}
        variant={selected.length === 0 ? 'filled' : 'outlined'}
        aria-pressed={selected.length === 0}
        onClick={() => onChange([])}
      />
      {areas.map((area) => {
        const active = selected.includes(area.id);
        return (
          <Chip
            key={area.id}
            label={`${area.label} (${area.services.length})`}
            color={active ? 'primary' : 'default'}
            variant={active ? 'filled' : 'outlined'}
            aria-pressed={active}
            onClick={() => toggle(area.id)}
          />
        );
      })}
    </Stack>
  );
}
