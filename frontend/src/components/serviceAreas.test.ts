import { describe, expect, it } from 'vitest';
import { service, serviceAreas } from '../test/fixtures';
import { groupByArea } from './serviceAreas';

const names = (groups: ReturnType<typeof groupByArea>) => groups.map((g) => [g.area.id, g.services.map((s) => s.name)]);

describe('groupByArea', () => {
  it('ordnet nach Bereichs-Reihenfolge, lässt leere Bereiche weg und zeigt Mehrfachzuordnungen in jedem Bereich', () => {
    const services = [
      service('api', true, [], ['business']),
      service('debug', true),
      service('mq', false, [], ['infra', 'business']),
    ];
    expect(names(groupByArea(services, serviceAreas))).toEqual([
      ['business', ['api', 'mq']],
      ['infra', ['mq']],
      ['others', ['debug']],
    ]);
  });

  it('lässt others weg, wenn alle Services zugeordnet sind', () => {
    expect(names(groupByArea([service('api', true, [], ['infra'])], serviceAreas))).toEqual([['infra', ['api']]]);
  });

  it('ordnet Services mit unbekannter Bereichs-ID others zu', () => {
    expect(names(groupByArea([service('x', true, [], ['gibts-nicht'])], serviceAreas))).toEqual([['others', ['x']]]);
    expect(names(groupByArea([service('x', true, [], ['gibts-nicht'])], []))).toEqual([['others', ['x']]]);
  });
});
