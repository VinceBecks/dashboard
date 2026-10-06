import { useEffect, useReducer } from 'react';
import { applyDelete, applyUpsert } from '../api/state';
import type { DashboardObject, ObjectHeader, Snapshot } from '../api/types';

export const EVENTS_URL = '/api/events';

/** connecting: noch kein Snapshot; open: live; reconnecting: Verbindung weg, Daten evtl. veraltet. */
export type ConnectionState = 'connecting' | 'open' | 'reconnecting';

interface State {
  data?: Snapshot;
  connection: ConnectionState;
}

type Msg =
  | { type: 'snapshot'; data: Snapshot }
  | { type: 'upsert'; obj: DashboardObject }
  | { type: 'delete'; header: ObjectHeader }
  | { type: 'error' };

function reducer(state: State, msg: Msg): State {
  switch (msg.type) {
    case 'snapshot':
      return { data: msg.data, connection: 'open' };
    case 'upsert':
      return state.data ? { ...state, data: applyUpsert(state.data, msg.obj) } : state;
    case 'delete':
      return state.data ? { ...state, data: applyDelete(state.data, msg.header) } : state;
    case 'error':
      return { ...state, connection: state.data ? 'reconnecting' : 'connecting' };
  }
}

/**
 * Hält den Dashboard-Zustand über GET /api/events aktuell: Das erste Event ist immer ein kompletter Snapshot,
 * danach kommen nur Änderungen. Nach einem Verbindungsabbruch verbindet sich der Browser selbst neu und erhält
 * wieder einen Snapshot.
 */
export function useDashboardEvents(): State {
  const [state, dispatch] = useReducer(reducer, { connection: 'connecting' });

  useEffect(() => {
    const source = new EventSource(EVENTS_URL);
    const parse = <T,>(e: Event) => JSON.parse((e as MessageEvent<string>).data) as T;
    source.addEventListener('snapshot', (e) => dispatch({ type: 'snapshot', data: parse<Snapshot>(e) }));
    source.addEventListener('upsert', (e) => dispatch({ type: 'upsert', obj: parse<DashboardObject>(e) }));
    source.addEventListener('delete', (e) => dispatch({ type: 'delete', header: parse<ObjectHeader>(e) }));
    source.onerror = () => dispatch({ type: 'error' });
    return () => source.close();
  }, []);

  return state;
}
