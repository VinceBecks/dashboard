import { ButtonBase, Stack, Tooltip } from '@mui/material';
import type { ServiceObject } from '../api/types';
import { ServiceDot } from './ServiceDot';

interface Props {
  services: ServiceObject[];
  onSelect: (name: string) => void;
}

/** Je Anwendungs-Service ein klickbarer Punkt (grün: mindestens ein Replica läuft). */
export function ServiceDots({ services, onSelect }: Props) {
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }} role="list" aria-label="Services">
      {services.map((svc) => (
        <Tooltip key={svc.name} title={svc.name}>
          <ButtonBase
            role="listitem"
            aria-label={`${svc.name}: ${svc.running ? 'läuft' : 'läuft nicht'}`}
            onClick={() => onSelect(svc.name)}
            sx={{ p: 0.5, borderRadius: '50%' }}
          >
            <ServiceDot service={svc} />
          </ButtonBase>
        </Tooltip>
      ))}
    </Stack>
  );
}
