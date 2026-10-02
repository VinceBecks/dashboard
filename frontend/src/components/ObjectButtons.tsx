import { Stack } from '@mui/material';
import type { Action, Link, ObjectRef } from '../api/types';
import { ActionButtons } from './ActionButtons';
import { LinkButtons } from './LinkButtons';

interface Props {
  actions: Action[];
  links: Link[];
  object: ObjectRef;
  onExecuted?: () => void;
}

/** Aktionen und Links eines Objekts in einer Zeile. */
export function ObjectButtons({ actions, links, object, onExecuted }: Props) {
  if (actions.length === 0 && links.length === 0) {
    return null;
  }
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
      <ActionButtons actions={actions} object={object} onExecuted={onExecuted} />
      <LinkButtons links={links} />
    </Stack>
  );
}
