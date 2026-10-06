import { ButtonBase, Card, CardContent, Link, Stack, Tooltip, Typography } from '@mui/material';
import { useState } from 'react';
import { refOf } from '../api/state';
import type { NamespaceObject } from '../api/types';
import { CopyButton } from './CopyButton';
import { ObjectButtons } from './ObjectButtons';
import { ServiceDialog } from './ServiceDialog';
import { ServiceDot } from './ServiceDot';

/**
 * Kachel einer Feature-Umgebung: Name mit Aktionen und Links, die Ingress-URLs, die NodePort-Services mit
 * kopierbaren Adressen und je Anwendungs-Service ein Punkt (grün: mindestens ein Replica läuft). Ein Klick auf
 * einen Punkt öffnet die Details des Services.
 */
export function NamespaceCard({ namespace }: { namespace: NamespaceObject }) {
  const [selected, setSelected] = useState<string>();
  const services = namespace.services ?? [];
  const ingresses = namespace.ingresses ?? [];
  const nodePortServices = namespace.nodePortServices ?? [];

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardContent>
        <Stack spacing={1.5}>
          <Typography variant="h6" component="h3" sx={{ wordBreak: 'break-all' }}>
            {namespace.name}
          </Typography>
          <ObjectButtons actions={namespace.actions} links={namespace.links} object={refOf(namespace)} />

          {ingresses.map((ing) => (
            <Stack key={ing.name} spacing={0.5}>
              {ing.urls.map((url) => (
                <Link key={url} href={url} target="_blank" rel="noopener noreferrer" variant="body2" sx={{ wordBreak: 'break-all' }}>
                  {url}
                </Link>
              ))}
              <ObjectButtons actions={ing.actions} links={ing.links} object={refOf(ing)} />
            </Stack>
          ))}

          {nodePortServices.map((np) => (
            <Stack key={np.name} spacing={0.5}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                <Typography variant="body2" sx={{ fontWeight: 500, wordBreak: 'break-all' }}>
                  {np.name}
                </Typography>
                <ObjectButtons actions={np.actions} links={np.links} object={refOf(np)} />
              </Stack>
              {np.addresses.map((address) => (
                <Stack key={address} direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                  <Typography variant="body2" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
                    {address}
                  </Typography>
                  <CopyButton text={address} label={`${address} kopieren`} />
                </Stack>
              ))}
            </Stack>
          ))}

          {services.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              Keine Anwendungs-Services
            </Typography>
          ) : (
            <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }} role="list" aria-label="Services">
              {services.map((svc) => (
                <Tooltip key={svc.name} title={svc.name}>
                  <ButtonBase
                    role="listitem"
                    aria-label={`${svc.name}: ${svc.running ? 'läuft' : 'läuft nicht'}`}
                    onClick={() => setSelected(svc.name)}
                    sx={{ p: 0.5, borderRadius: '50%' }}
                  >
                    <ServiceDot service={svc} />
                  </ButtonBase>
                </Tooltip>
              ))}
            </Stack>
          )}
        </Stack>
      </CardContent>
      {selected && (
        <ServiceDialog
          name={selected}
          service={services.find((s) => s.name === selected)}
          onClose={() => setSelected(undefined)}
        />
      )}
    </Card>
  );
}
