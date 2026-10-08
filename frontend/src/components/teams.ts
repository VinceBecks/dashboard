import type { NamespaceObject } from '../api/types';

/** Auswahlwert „Ohne Team“ im Filter; "-" ist keine gültige Team-ID (kein gültiger Label-Wert). */
export const NO_TEAM = '-';

/** Namespaces, die zur Auswahl passen. Ohne Auswahl alle; „Ohne Team“ trifft Namespaces ohne (bekanntes) Team. */
export function filterByTeams(namespaces: NamespaceObject[], selected: string[]): NamespaceObject[] {
  if (selected.length === 0) {
    return namespaces;
  }
  return namespaces.filter((ns) => selected.includes(ns.team ?? NO_TEAM));
}
