import { Chip, Stack, Tooltip, keyframes } from '@mui/material';
import { LoaderCircle } from 'lucide-react';
import type { Pipeline } from '../api/types';

const spin = keyframes`from { transform: rotate(0deg); } to { transform: rotate(360deg); }`;

/** Je laufender GitLab-Pipeline ein Chip „Pipeline läuft“, der in GitLab zur Pipeline springt. */
export function PipelineBadges({ pipelines }: { pipelines: Pipeline[] }) {
  if (pipelines.length === 0) {
    return null;
  }
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }} role="group" aria-label="Laufende Pipelines">
      {pipelines.map((p) => (
        <Tooltip key={p.action} title={`${p.title} – gestartet ${formatTime(p.startedAt)}`}>
          <Chip
            component="a"
            href={p.url}
            target="_blank"
            rel="noopener noreferrer"
            clickable
            size="small"
            color="info"
            variant="outlined"
            label="Pipeline läuft"
            aria-label={`Pipeline ${p.title} läuft – in GitLab öffnen`}
            icon={<LoaderCircle size={14} />}
            sx={{ '& .MuiChip-icon': { animation: `${spin} 1.2s linear infinite` } }}
          />
        </Tooltip>
      ))}
    </Stack>
  );
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' });
}
