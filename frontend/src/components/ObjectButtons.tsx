import { Divider, Stack } from '@mui/material';
import type { Action, Link, ObjectRef } from '../api/types';
import { ActionButtons } from './ActionButtons';
import { LinkButtons } from './LinkButtons';

interface Props {
  actions: Action[];
  links: Link[];
  object: ObjectRef;
  onExecuted?: () => void;
}

/**
 * Aktionen und Links eines Objekts in einer Zeile: Aktionen links, Links rechtsbündig und durch einen
 * Trennstrich abgesetzt, damit beide Gruppen unterscheidbar bleiben.
 */
export function ObjectButtons({ actions, links, object, onExecuted }: Props) {
  if (actions.length === 0 && links.length === 0) {
    return null;
  }
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
      <ActionButtons actions={actions} object={object} onExecuted={onExecuted} />
      {links.length > 0 && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', ml: 'auto' }}>
          {actions.length > 0 && <Divider orientation="vertical" flexItem />}
          <LinkButtons links={links} />
        </Stack>
      )}
    </Stack>
  );
}
