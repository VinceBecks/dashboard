import { Box, Card, CardContent, Link, Stack, Tab, Tabs, Typography } from '@mui/material';
import { useState } from 'react';
import type { NamespaceObject, ServiceArea } from '../api/types';
import { CopyButton } from './CopyButton';
import { ObjectButtons } from './ObjectButtons';
import { ServiceDialog } from './ServiceDialog';
import { ServiceDots } from './ServiceDots';
import { groupByArea, type AreaGroup } from './serviceAreas';

interface Props {
  namespace: NamespaceObject;
  /** Service-Bereiche des Projekts; daraus entstehen die Reiter über den Service-Punkten. */
  serviceAreas?: ServiceArea[];
}

/**
 * Kachel einer Feature-Umgebung: Name mit Aktionen und Links, die Ingress-URLs, die NodePort-Services mit
 * kopierbaren Adressen und je Anwendungs-Service ein Punkt (grün: mindestens ein Replica läuft), aufgeteilt in
 * Reiter je Service-Bereich. Ein Klick auf einen Punkt öffnet die Details des Services.
 */
export function NamespaceCard({ namespace, serviceAreas = [] }: Props) {
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
          <ObjectButtons object={namespace} />

          {ingresses.map((ing) => (
            <Stack key={ing.name} spacing={0.5}>
              {ing.urls.map((url) => (
                <Link key={url} href={url} target="_blank" rel="noopener noreferrer" variant="body2" sx={{ wordBreak: 'break-all' }}>
                  {url}
                </Link>
              ))}
              <ObjectButtons object={ing} />
            </Stack>
          ))}

          {nodePortServices.map((np) => (
            <Stack key={np.name} spacing={0.5}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                <Typography variant="body2" sx={{ fontWeight: 500, wordBreak: 'break-all' }}>
                  {np.name}
                </Typography>
                <ObjectButtons object={np} />
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
            <ServiceAreaTabs groups={groupByArea(services, serviceAreas)} onSelect={setSelected} />
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

/**
 * Reiter je Service-Bereich mit dessen Punkten. Nur Bereiche mit Services erscheinen; bei nur einem Bereich entfällt
 * die Reiterleiste. Verschwindet der gewählte Bereich, gilt der erste.
 */
function ServiceAreaTabs({ groups, onSelect }: { groups: AreaGroup[]; onSelect: (name: string) => void }) {
  const [tab, setTab] = useState<string>();
  if (groups.length <= 1) {
    return <ServiceDots services={groups[0]?.services ?? []} onSelect={onSelect} />;
  }
  const active = groups.find((g) => g.area.id === tab) ?? groups[0];
  return (
    <Box>
      <Tabs
        value={active.area.id}
        onChange={(_, id: string) => setTab(id)}
        variant="scrollable"
        scrollButtons="auto"
        aria-label="Service-Bereiche"
        sx={{ minHeight: 32, mb: 1, '& .MuiTab-root': { minHeight: 32, py: 0.5, px: 1.5, textTransform: 'none' } }}
      >
        {groups.map((g) => (
          <Tab key={g.area.id} value={g.area.id} label={`${g.area.label} (${g.services.length})`} />
        ))}
      </Tabs>
      <ServiceDots services={active.services} onSelect={onSelect} />
    </Box>
  );
}
