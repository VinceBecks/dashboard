import {
  Alert,
  Box,
  Breadcrumbs,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Divider,
  Grid,
  Link as MuiLink,
  List,
  ListItem,
  ListItemText,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import type { ReactNode } from 'react';
import { Link, useParams } from 'react-router';
import { ApiError } from '../api/client';
import type { Ingress, NamespaceResponse, ObjectKind, ObjectRef, Pod, Service } from '../api/types';
import { ActionButtons } from '../components/ActionButtons';
import { Loading, PollError } from '../components/StatusViews';
import { usePolling } from '../hooks/usePolling';

/** Namespace-Seite: Aktionen, Ingresses, NodePort-Services und Anwendungs-Services mit ihren Replicas. */
export function NamespacePage() {
  const { project = '', namespace = '' } = useParams();
  const url = `/api/projects/${encodeURIComponent(project)}/namespaces/${encodeURIComponent(namespace)}`;
  const { data, error, loading, refresh } = usePolling<NamespaceResponse>(url);

  const notFound = error instanceof ApiError && error.status === 404;
  const ref = (kind: ObjectKind, name: string): ObjectRef => ({ kind, namespace, name });

  return (
    <>
      <Breadcrumbs sx={{ mb: 2 }}>
        <MuiLink component={Link} to="/" underline="hover" color="inherit">
          Projekte
        </MuiLink>
        <Typography color="text.secondary">{project}</Typography>
        <Typography color="text.primary">{namespace}</Typography>
      </Breadcrumbs>

      {notFound ? (
        <Alert severity="info">
          Die Feature-Umgebung „{namespace}“ existiert nicht (mehr).{' '}
          <MuiLink component={Link} to="/">
            Zur Übersicht
          </MuiLink>
        </Alert>
      ) : (
        <>
          {error && <PollError error={error} />}
          {loading && <Loading />}
        </>
      )}

      {data && !notFound && (
        <Stack spacing={4}>
          <Box>
            <Typography variant="h4" component="h1" gutterBottom>
              {data.namespace.name}
            </Typography>
            <ActionButtons actions={data.namespace.actions} object={ref('namespace', data.namespace.name)} onExecuted={refresh} />
          </Box>

          <Section title="Ingresses" empty={data.ingresses.length === 0}>
            <Stack spacing={2}>
              {data.ingresses.map((ing) => (
                <IngressCard key={ing.name} ingress={ing} object={ref('ingress', ing.name)} onExecuted={refresh} />
              ))}
            </Stack>
          </Section>

          <NodePortSection services={data.services.filter((s) => s.type === 'NodePort')} objectRef={ref} onExecuted={refresh} />

          <Section title="Anwendungs-Services" empty={!data.services.some((s) => s.type === 'ClusterIP')}>
            <Grid container spacing={2}>
              {data.services
                .filter((s) => s.type === 'ClusterIP')
                .map((svc) => (
                  <Grid key={svc.name} size={{ xs: 12, md: 6, xl: 4 }}>
                    <ServiceCard service={svc} pods={data.pods} object={ref('service', svc.name)} onExecuted={refresh} />
                  </Grid>
                ))}
            </Grid>
          </Section>
        </Stack>
      )}
    </>
  );
}

function Section({ title, empty, children }: { title: string; empty: boolean; children: ReactNode }) {
  return (
    <Box component="section">
      <Typography variant="h5" component="h2" gutterBottom>
        {title}
      </Typography>
      {empty ? <Typography color="text.secondary">Keine vorhanden</Typography> : children}
    </Box>
  );
}

interface ObjectProps {
  object: ObjectRef;
  onExecuted: () => void;
}

function IngressCard({ ingress, object, onExecuted }: ObjectProps & { ingress: Ingress }) {
  const rows = ingress.rules.flatMap((r) => r.paths.map((p) => ({ host: r.host, ...p })));
  return (
    <Card variant="outlined">
      <CardHeader title={ingress.name} slotProps={{ title: { variant: 'h6' } }} />
      <CardContent sx={{ pt: 0 }}>
        <ActionButtons actions={ingress.actions} object={object} onExecuted={onExecuted} />
        <TableContainer sx={{ mt: 1 }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Host</TableCell>
                <TableCell>Pfad</TableCell>
                <TableCell>Pfad-Typ</TableCell>
                <TableCell>Backend</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((r, i) => (
                <TableRow key={i}>
                  <TableCell>
                    {r.host ? (
                      <MuiLink href={`//${r.host}${r.path || '/'}`} target="_blank" rel="noreferrer">
                        {r.host}
                      </MuiLink>
                    ) : (
                      '*'
                    )}
                  </TableCell>
                  <TableCell>{r.path || '/'}</TableCell>
                  <TableCell>{r.pathType}</TableCell>
                  <TableCell>
                    {r.backend.serviceName}:{r.backend.resolvedServicePortNumber ?? r.backend.servicePortName ?? '?'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      </CardContent>
    </Card>
  );
}

function NodePortSection({
  services,
  objectRef,
  onExecuted,
}: {
  services: Service[];
  objectRef: (kind: ObjectKind, name: string) => ObjectRef;
  onExecuted: () => void;
}) {
  return (
    <Section title="NodePort-Services" empty={services.length === 0}>
      <TableContainer component={Paper} variant="outlined">
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Service</TableCell>
              <TableCell>Ports (Port → NodePort)</TableCell>
              <TableCell>Aktionen</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {services.map((s) => (
              <TableRow key={s.name}>
                <TableCell>{s.name}</TableCell>
                <TableCell>
                  {s.ports.map((p) => (
                    <Chip
                      key={`${p.name}-${p.port}`}
                      size="small"
                      sx={{ mr: 0.5 }}
                      label={`${p.name ? `${p.name}: ` : ''}${p.port} → ${p.nodePort ?? '-'}`}
                    />
                  ))}
                </TableCell>
                <TableCell>
                  <ActionButtons actions={s.actions} object={objectRef('service', s.name)} onExecuted={onExecuted} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Section>
  );
}

function ServiceCard({ service, pods, object, onExecuted }: ObjectProps & { service: Service; pods: Pod[] }) {
  const replicas = service.replicas
    .map((name) => pods.find((p) => p.name === name))
    .filter((p): p is Pod => p !== undefined);
  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader title={service.name} slotProps={{ title: { variant: 'h6' } }} />
      <CardContent sx={{ pt: 0 }}>
        <ActionButtons actions={service.actions} object={object} onExecuted={onExecuted} />
        <Divider sx={{ my: 1.5 }} />
        <Typography variant="subtitle2">Replicas ({replicas.length})</Typography>
        {replicas.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Keine Replicas
          </Typography>
        ) : (
          <List dense disablePadding>
            {replicas.map((pod) => (
              // Aktionen auf Pods werden bewusst (noch) nicht angezeigt.
              <ListItem key={pod.name} disableGutters secondaryAction={<PodStatus pod={pod} />}>
                <ListItemText
                  primary={pod.name}
                  secondary={pod.containers.map((c) => c.image).join(', ')}
                  slotProps={{ secondary: { sx: { wordBreak: 'break-all' } } }}
                />
              </ListItem>
            ))}
          </List>
        )}
      </CardContent>
    </Card>
  );
}

function PodStatus({ pod }: { pod: Pod }) {
  const color = pod.ready ? 'success' : pod.phase === 'Failed' ? 'error' : 'warning';
  return <Chip size="small" color={color} label={pod.ready ? 'Ready' : pod.phase || 'Unbekannt'} />;
}
