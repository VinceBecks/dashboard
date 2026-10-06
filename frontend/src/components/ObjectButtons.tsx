import { Divider, Stack } from '@mui/material';
import { refOf } from '../api/state';
import type { DashboardObject } from '../api/types';
import { ActionButtons } from './ActionButtons';
import { CopyCommandButtons } from './CopyCommandButtons';
import { LinkButtons } from './LinkButtons';

/**
 * Aktionen, Kopier-Befehle und Links eines Objekts in einer Zeile: Aktionen links, Kopier-Befehle und Links
 * rechtsbündig und durch einen Trennstrich abgesetzt, damit beide Gruppen unterscheidbar bleiben.
 */
export function ObjectButtons({ object }: { object: DashboardObject }) {
  const { actions, links, copyCommands } = object;
  if (actions.length === 0 && links.length === 0 && copyCommands.length === 0) {
    return null;
  }
  const hasRight = links.length > 0 || copyCommands.length > 0;
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
      <ActionButtons actions={actions} object={refOf(object)} />
      {hasRight && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', ml: 'auto' }}>
          {actions.length > 0 && <Divider orientation="vertical" flexItem />}
          <CopyCommandButtons commands={copyCommands} />
          <LinkButtons links={links} />
        </Stack>
      )}
    </Stack>
  );
}
