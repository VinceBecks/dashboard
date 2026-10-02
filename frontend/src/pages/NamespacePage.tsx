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
import { Link, useParams, useSearchParams } from 'react-router';
import { ApiError } from '../api/client';
import type { Ingress, NamespaceResponse, ObjectKind, ObjectRef, Pod, Service } from '../api/types';
import { LinkButtons } from '../components/LinkButtons';
import { ObjectButtons } from '../components/ObjectButtons';
import { ServiceAreaFilter } from '../components/ServiceAreaFilter';
import { Loading, PollError } from '../components/StatusViews';
import { usePolling } from '../hooks/usePolling';

/** Namespace-Seite: Aktionen, Ingresses, NodePort-Services und Anwendungs-Services (je Service-Bereich) mit ihren Replicas. */
export function NamespacePage() {
  const { project = '', namespace = '' } = useParams();
  const url = `/api/projects/${encodeURIComponent(project)}/namespaces/${encodeURIComponent(namespace)}`;
  const { data, error, loading, refresh } = usePolling<NamespaceResponse>(url);

  // Ausgewählte Service-Bereiche stehen in der URL (?area=a&area=b), damit sie Neuladen und Teilen überstehen.
  const [searchParams, setSearchParams] = useSearchParams();
  const knownAreas = new Set(data?.serviceAreas.map((a) => a.id));
  const selectedAreas = searchParams.getAll('area').filter((id) => knownAreas.has(id));
  const setSelectedAreas = (ids: string[]) =>
    setSearchParams(
      (params) => {
        params.delete('area');
        ids.forEach((id) => params.append('area', id));
        return params;
      },
      { replace: true },
    );
  const visibleAreas = data?.serviceAreas.filter((a) => selectedAreas.length === 0 || selectedAreas.includes(a.id)) ?? [];

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
            <ObjectButtons
              actions={data.namespace.actions}
              links={data.namespace.links}
              object={ref('namespace', data.namespace.name)}
              onExecuted={refresh}
            />
          </Box>

          <Section title="Ingresses" empty={data.ingresses.length === 0}>
            <Stack spacing={2}>
              {data.ingresses.map((ing) => (
                <IngressCard key={ing.name} ingress={ing} object={ref('ingress', ing.name)} onExecuted={refresh} />
              ))}
            </Stack>
          </Section>

          <NodePortSection services={data.services.filter((s) => s.type === 'NodePort')} objectRef={ref} onExecuted={refresh} />

          {data.serviceAreas.length > 1 && (
            <ServiceAreaFilter areas={data.serviceAreas} selected={selectedAreas} onChange={setSelectedAreas} />
          )}

          {visibleAreas.map((area) => {
            const services = area.services
              .map((name) => data.services.find((s) => s.name === name))
              .filter((s): s is Service => s !== undefined);
            return (
              <Section key={area.id} title={area.label} empty={services.length === 0}>
                <Grid container spacing={2}>
                  {services.map((svc) => (
                    <Grid key={svc.name} size={{ xs: 12, md: 6, xl: 4 }}>
                      <ServiceCard service={svc} pods={data.pods} object={ref('service', svc.name)} onExecuted={refresh} />
                    </Grid>
                  ))}
                </Grid>
              </Section>
            );
          })}
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
        <ObjectButtons actions={ingress.actions} links={ingress.links} object={object} onExecuted={onExecuted} />
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
                  <ObjectButtons actions={s.actions} links={s.links} object={objectRef('service', s.name)} onExecuted={onExecuted} />
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
        <ObjectButtons actions={service.actions} links={service.links} object={object} onExecuted={onExecuted} />
        <Divider sx={{ my: 1.5 }} />
        <Typography variant="subtitle2">Replicas ({replicas.length})</Typography>
        {replicas.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Keine Replicas
          </Typography>
        ) : (
          <List dense disablePadding>
            {replicas.map((pod) => (
              // Pod-Links (z. B. Logs) werden gezeigt, Pod-Aktionen nicht.
              <ListItem
                key={pod.name}
                disableGutters
                secondaryAction={
                  <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                    <LinkButtons links={pod.links} />
                    <PodStatus pod={pod} />
                  </Stack>
                }
              >
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
