import { useCallback, useEffect, useState } from 'react';

function read(key: string): string[] {
  return new URLSearchParams(window.location.search).getAll(key);
}

/**
 * Liste von Werten im Query-Parameter key (?key=a&key=b). Änderungen ersetzen den Eintrag in der Browser-Historie,
 * damit die Auswahl Reload und Teilen des Links übersteht, ohne die Zurück-Taste zu füllen.
 */
export function useUrlList(key: string): [string[], (values: string[]) => void] {
  const [values, setValues] = useState(() => read(key));

  useEffect(() => {
    const onPop = () => setValues(read(key));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [key]);

  const update = useCallback(
    (next: string[]) => {
      const params = new URLSearchParams(window.location.search);
      params.delete(key);
      next.forEach((v) => params.append(key, v));
      const query = params.toString();
      window.history.replaceState(window.history.state, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`);
      setValues(next);
    },
    [key],
  );

  return [values, update];
}
