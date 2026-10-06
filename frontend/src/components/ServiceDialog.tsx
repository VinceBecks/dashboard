import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Stack,
  Typography,
} from '@mui/material';
import { refOf } from '../api/state';
import type { ServiceObject } from '../api/types';
import { ObjectButtons } from './ObjectButtons';
import { PodStatus } from './PodStatus';
import { ServiceDot } from './ServiceDot';

interface Props {
  name: string;
  /** Aktueller Stand aus dem Event-Stream; undefined, wenn der Service inzwischen entfernt wurde. */
  service?: ServiceObject;
  onClose: () => void;
}

/** Details eines Anwendungs-Services: seine Aktionen und Links, darunter je Replica Status, Container und Aktionen. */
export function ServiceDialog({ name, service, onClose }: Props) {
  const pods = service?.pods ?? [];
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {service && <ServiceDot service={service} />}
        {name}
      </DialogTitle>
      <DialogContent dividers>
        {!service ? (
          <Alert severity="info">Der Service existiert nicht mehr.</Alert>
        ) : (
          <Stack spacing={2}>
            <ObjectButtons actions={service.actions} links={service.links} object={refOf(service)} />
            <Divider />
            <Typography variant="subtitle2">Replicas</Typography>
            {pods.length === 0 && (
              <Typography variant="body2" color="text.secondary">
                Keine Replicas
              </Typography>
            )}
            {pods.map((pod) => (
              <Stack key={pod.name} spacing={1} sx={{ border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5 }}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <Typography variant="body2" sx={{ fontWeight: 500, wordBreak: 'break-all' }}>
                    {pod.name}
                  </Typography>
                  <PodStatus pod={pod} />
                </Stack>
                {pod.containers.map((c) => (
                  <Typography key={c.name} variant="caption" color="text.secondary" sx={{ wordBreak: 'break-all' }}>
                    {c.name}: {c.image}
                  </Typography>
                ))}
                <ObjectButtons actions={pod.actions} links={pod.links} object={refOf(pod)} />
              </Stack>
            ))}
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Schließen</Button>
      </DialogActions>
    </Dialog>
  );
}
