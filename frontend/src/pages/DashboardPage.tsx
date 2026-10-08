import { Box, Stack, Typography } from '@mui/material';
import type { ProjectSnapshot, Snapshot, Team } from '../api/types';
import { NamespaceCard } from '../components/NamespaceCard';
import { Loading } from '../components/StatusViews';
import { TeamFilter } from '../components/TeamFilter';
import { filterByTeams } from '../components/teams';
import { useUrlList } from '../hooks/useUrlList';

/** Einzige Seite: je Projekt die Kacheln seiner Feature-Umgebungen. */
export function DashboardPage({ data }: { data?: Snapshot }) {
  if (!data) {
    return <Loading />;
  }
  return (
    <Stack spacing={4}>
      {data.projects.map((project) => (
        <ProjectSection key={project.name} project={project} teams={data.teams ?? []} />
      ))}
    </Stack>
  );
}

/** Ein Projekt: Überschrift, Team-Filter (falls Teams konfiguriert sind) und die Kacheln. */
function ProjectSection({ project, teams }: { project: ProjectSnapshot; teams: Team[] }) {
  const [selected, setSelected] = useUrlList(`team.${project.name}`);
  const namespaces = filterByTeams(project.namespaces, selected);
  const teamLabels = new Map(teams.map((t) => [t.id, t.label]));

  return (
    <section aria-label={project.name}>
      <Typography variant="h5" component="h2" gutterBottom>
        {project.name}
      </Typography>
      {teams.length > 0 && project.namespaces.length > 0 && (
        <TeamFilter teams={teams} selected={selected} onChange={setSelected} />
      )}
      {project.namespaces.length === 0 ? (
        <Typography color="text.secondary">Keine Feature-Umgebungen</Typography>
      ) : namespaces.length === 0 ? (
        <Typography color="text.secondary">Keine Feature-Umgebungen für die gewählten Teams</Typography>
      ) : (
        <Box sx={{ display: 'grid', gap: 2, gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))' }}>
          {namespaces.map((ns) => (
            <NamespaceCard
              key={ns.name}
              namespace={ns}
              serviceAreas={project.serviceAreas}
              teamLabel={ns.team ? teamLabels.get(ns.team) : undefined}
            />
          ))}
        </Box>
      )}
    </section>
  );
}
