import { Card, CardContent, CardHeader, Chip, Grid, List, ListItemButton, ListItemText, Typography } from '@mui/material';
import { Link } from 'react-router';
import type { ProjectsResponse } from '../api/types';
import { Loading, PollError } from '../components/StatusViews';
import { usePolling } from '../hooks/usePolling';

/** Übersicht: Projekte und ihre Feature-Umgebungen. */
export function OverviewPage() {
  const { data, error, loading } = usePolling<ProjectsResponse>('/api/projects');

  return (
    <>
      <Typography variant="h4" component="h1" gutterBottom>
        Projekte
      </Typography>
      {error && <PollError error={error} />}
      {loading && <Loading />}
      <Grid container spacing={2}>
        {data?.projects.map((p) => (
          <Grid key={p.name} size={{ xs: 12, md: 6, lg: 4 }}>
            <Card variant="outlined" sx={{ height: '100%' }}>
              <CardHeader
                title={p.name}
                action={<Chip label={`${p.namespaces.length} Umgebungen`} size="small" />}
              />
              <CardContent sx={{ pt: 0 }}>
                {p.namespaces.length === 0 ? (
                  <Typography color="text.secondary">Keine Feature-Umgebungen</Typography>
                ) : (
                  <List dense disablePadding>
                    {p.namespaces.map((ns) => (
                      <ListItemButton
                        key={ns.name}
                        component={Link}
                        to={`/projects/${encodeURIComponent(p.name)}/namespaces/${encodeURIComponent(ns.name)}`}
                      >
                        <ListItemText primary={ns.name} />
                      </ListItemButton>
                    ))}
                  </List>
                )}
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
    </>
  );
}
