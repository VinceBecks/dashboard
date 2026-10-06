import { Box, Stack, Typography } from '@mui/material';
import type { Snapshot } from '../api/types';
import { NamespaceCard } from '../components/NamespaceCard';
import { Loading } from '../components/StatusViews';

/** Einzige Seite: je Projekt die Kacheln seiner Feature-Umgebungen. */
export function DashboardPage({ data }: { data?: Snapshot }) {
  if (!data) {
    return <Loading />;
  }
  return (
    <Stack spacing={4}>
      {data.projects.map((project) => (
        <section key={project.name} aria-label={project.name}>
          <Typography variant="h5" component="h2" gutterBottom>
            {project.name}
          </Typography>
          {project.namespaces.length === 0 ? (
            <Typography color="text.secondary">Keine Feature-Umgebungen</Typography>
          ) : (
            <Box
              sx={{
                display: 'grid',
                gap: 2,
                gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
              }}
            >
              {project.namespaces.map((ns) => (
                <NamespaceCard key={ns.name} namespace={ns} serviceAreas={project.serviceAreas} />
              ))}
            </Box>
          )}
        </section>
      ))}
    </Stack>
  );
}
