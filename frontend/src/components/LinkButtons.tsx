import { IconButton, Stack, Tooltip } from '@mui/material';
import type { Link } from '../api/types';
import { ActionIcon } from './ActionIcon';

/** Icon-Links eines Objekts (z. B. Absprung nach Kibana); URL, Icon und Titel kommen fertig aus dem Backend. */
export function LinkButtons({ links }: { links: Link[] }) {
  if (links.length === 0) {
    return null;
  }
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }}>
      {links.map((l) => (
        <Tooltip key={l.id} title={l.title}>
          <IconButton size="small" aria-label={l.title} href={l.url} target="_blank" rel="noopener noreferrer">
            <ActionIcon name={l.lucideIcon} color={l.lucideIconColor} size={20} />
          </IconButton>
        </Tooltip>
      ))}
    </Stack>
  );
}
