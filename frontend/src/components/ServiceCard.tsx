import {
  Box,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Divider,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import { ArrowLeft } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import type { ObjectRef, Pod, Service } from '../api/types';
import { LinkButtons } from './LinkButtons';
import { ObjectButtons } from './ObjectButtons';

interface Props {
  service: Service;
  pods: Pod[];
  namespace: string;
  onExecuted: () => void;
}

/**
 * Kachel eines Anwendungs-Services. Vorderseite: Aktionen/Links des Services und seine Replicas.
 * Ein Klick auf ein Replica dreht die Kachel um; die Rückseite zeigt Infos, Aktionen und Links des Pods.
 */
export function ServiceCard({ service, pods, namespace, onExecuted }: Props) {
  // Gemerkt wird nur der Name: Nach jedem Polling wird der Pod frisch aus den Daten gelesen.
  const [selectedPod, setSelectedPod] = useState<string>();
  const replicas = service.replicas
    .map((name) => pods.find((p) => p.name === name))
    .filter((p): p is Pod => p !== undefined);
  const flipped = selectedPod !== undefined;
  const ref = (kind: 'service' | 'pod', name: string): ObjectRef => ({ kind, namespace, name });

  return (
    <Box sx={{ perspective: '1500px', height: '100%' }}>
      <Box
        data-testid={`service-card-${service.name}`}
        data-flipped={flipped}
        sx={{
          display: 'grid',
          height: '100%',
          transformStyle: 'preserve-3d',
          transition: 'transform 0.5s',
          transform: flipped ? 'rotateY(180deg)' : 'none',
          '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
        }}
      >
        <Face hidden={flipped}>
          <CardHeader title={service.name} slotProps={{ title: { variant: 'h6', component: 'h3' } }} />
          <CardContent sx={{ pt: 0 }}>
            <ObjectButtons actions={service.actions} links={service.links} object={ref('service', service.name)} onExecuted={onExecuted} />
            <Divider sx={{ my: 1.5 }} />
            <Typography variant="subtitle2">Replicas ({replicas.length})</Typography>
            {replicas.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                Keine Replicas
              </Typography>
            ) : (
              <List dense disablePadding>
                {replicas.map((pod) => (
                  <ListItem
                    key={pod.name}
                    disablePadding
                    secondaryAction={
                      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                        <LinkButtons links={pod.links} />
                        <PodStatus pod={pod} />
                      </Stack>
                    }
                  >
                    <ListItemButton onClick={() => setSelectedPod(pod.name)} aria-label={`Details zu Pod ${pod.name}`}>
                      <ListItemText
                        primary={pod.name}
                        secondary={pod.containers.map((c) => c.image).join(', ')}
                        slotProps={{ secondary: { sx: { wordBreak: 'break-all' } } }}
                      />
                    </ListItemButton>
                  </ListItem>
                ))}
              </List>
            )}
          </CardContent>
        </Face>

        <Face hidden={!flipped} back>
          {flipped && (
            <PodDetails
              name={selectedPod}
              pod={pods.find((p) => p.name === selectedPod)}
              object={ref('pod', selectedPod)}
              onBack={() => setSelectedPod(undefined)}
              onExecuted={onExecuted}
            />
          )}
        </Face>
      </Box>
    </Box>
  );
}

/** Eine Seite der Kachel. Beide Seiten liegen im selben Grid-Feld, die Kachel ist so hoch wie die größere. */
function Face({ hidden, back = false, children }: { hidden: boolean; back?: boolean; children: ReactNode }) {
  return (
    <Card
      variant="outlined"
      aria-hidden={hidden}
      inert={hidden}
      sx={{
        gridArea: '1 / 1',
        backfaceVisibility: 'hidden',
        transform: back ? 'rotateY(180deg)' : 'none',
      }}
    >
      {children}
    </Card>
  );
}

interface PodDetailsProps {
  name: string;
  pod: Pod | undefined;
  object: ObjectRef;
  onBack: () => void;
  onExecuted: () => void;
}

function PodDetails({ name, pod, object, onBack, onExecuted }: PodDetailsProps) {
  return (
    <>
      <CardHeader
        avatar={
          <Tooltip title="Zurück zum Service">
            <IconButton size="small" aria-label="Zurück zum Service" onClick={onBack} edge="start">
              <ArrowLeft size={20} />
            </IconButton>
          </Tooltip>
        }
        title={name}
        subheader="Pod"
        slotProps={{ title: { variant: 'h6', component: 'h3', sx: { wordBreak: 'break-all' } } }}
      />
      <CardContent sx={{ pt: 0 }}>
        {pod === undefined ? (
          // Der Pod kann zwischenzeitlich gelöscht worden sein, z. B. durch einen Neustart.
          <Typography variant="body2" color="text.secondary">
            Der Pod existiert nicht mehr.
          </Typography>
        ) : (
          <Stack spacing={1.5}>
            <InfoRow label="Status">
              <Box>
                <PodStatus pod={pod} />
              </Box>
            </InfoRow>
            <InfoRow label="Container">
              <Stack spacing={1} divider={<Divider flexItem />}>
                {pod.containers.map((c) => (
                  <KeyValueList key={c.name}>
                    <KeyValue label="Name">{c.name}</KeyValue>
                    <KeyValue label="Image">{c.image}</KeyValue>
                    {c.ports && c.ports.length > 0 && (
                      <KeyValue label="Ports">
                        <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }}>
                          {c.ports.map((p) => (
                            <Chip
                              key={`${p.name}-${p.containerPort}`}
                              size="small"
                              variant="outlined"
                              label={`${p.name ? `${p.name}: ` : ''}${p.containerPort}${p.protocol && p.protocol !== 'TCP' ? `/${p.protocol}` : ''}`}
                            />
                          ))}
                        </Stack>
                      </KeyValue>
                    )}
                  </KeyValueList>
                ))}
              </Stack>
            </InfoRow>
            <Divider />
            <ObjectButtons actions={pod.actions} links={pod.links} object={object} onExecuted={onExecuted} />
          </Stack>
        )}
      </CardContent>
    </>
  );
}

/** Schlüssel-Wert-Paare als Beschreibungsliste: Schlüssel links, Wert rechts. */
function KeyValueList({ children }: { children: ReactNode }) {
  return (
    <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: 'auto 1fr', columnGap: 1.5, rowGap: 0.5, m: 0 }}>
      {children}
    </Box>
  );
}

function KeyValue({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <Typography component="dt" variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography component="dd" variant="body2" sx={{ m: 0, wordBreak: 'break-all' }}>
        {children}
      </Typography>
    </>
  );
}

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box>
      <Typography variant="caption" color="text.secondary">
        {label}
      </Typography>
      {children}
    </Box>
  );
}

function PodStatus({ pod }: { pod: Pod }) {
  const color = pod.ready ? 'success' : pod.phase === 'Failed' ? 'error' : 'warning';
  return <Chip size="small" color={color} label={pod.ready ? 'Ready' : pod.phase || 'Unbekannt'} />;
}
