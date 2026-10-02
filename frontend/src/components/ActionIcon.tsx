import { CircleHelp } from 'lucide-react';
import { DynamicIcon, iconNames, type IconName } from 'lucide-react/dynamic';

const knownIcons = new Set<string>(iconNames);

interface Props {
  name: string;
  color: string;
  size?: number;
}

/** Lucide-Icon anhand des Namens aus dem Backend; unbekannte Namen zeigen ein Platzhalter-Icon. */
export function ActionIcon({ name, color, size = 18 }: Props) {
  if (!knownIcons.has(name)) {
    return <CircleHelp color={color} size={size} aria-hidden />;
  }
  return <DynamicIcon name={name as IconName} color={color} size={size} aria-hidden />;
}
