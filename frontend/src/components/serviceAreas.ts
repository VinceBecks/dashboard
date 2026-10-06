import type { ServiceArea, ServiceObject } from '../api/types';

export const OTHERS_AREA_ID = 'others';

export interface AreaGroup {
  area: ServiceArea;
  services: ServiceObject[];
}

/**
 * Verteilt die Services auf die Bereiche in der Reihenfolge von areas. Leere Bereiche entfallen; ein Service in
 * mehreren Bereichen steht in jedem. Verweist ein Service auf einen unbekannten Bereich, landet er in „others“.
 */
export function groupByArea(services: ServiceObject[], areas: ServiceArea[]): AreaGroup[] {
  const known = new Set(areas.map((a) => a.id));
  const ids = (s: ServiceObject) => {
    const valid = (s.areas ?? []).filter((id) => known.has(id));
    return valid.length > 0 ? valid : [OTHERS_AREA_ID];
  };
  const others = areas.find((a) => a.id === OTHERS_AREA_ID) ?? { id: OTHERS_AREA_ID, label: 'Andere' };
  const ordered = known.has(OTHERS_AREA_ID) ? areas : [...areas, others];
  return ordered
    .map((area) => ({ area, services: services.filter((s) => ids(s).includes(area.id)) }))
    .filter((g) => g.services.length > 0);
}
