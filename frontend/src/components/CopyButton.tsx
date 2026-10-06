import { IconButton, Tooltip } from '@mui/material';
import { Check, Copy } from 'lucide-react';
import { useEffect, useState } from 'react';

/**
 * Kopiert text in die Zwischenablage. navigator.clipboard gibt es nur in sicheren Kontexten (HTTPS, localhost);
 * im Cluster läuft das Dashboard evtl. per HTTP, daher der Fallback über ein temporäres Textfeld.
 */
export async function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    if (!document.execCommand('copy')) {
      throw new Error('Kopieren nicht möglich');
    }
  } finally {
    document.body.removeChild(textarea);
  }
}

/** Icon-Button, der text kopiert und kurz einen Haken als Bestätigung zeigt. */
export function CopyButton({ text, label = 'In Zwischenablage kopieren' }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');

  useEffect(() => {
    if (state === 'idle') {
      return;
    }
    const timer = setTimeout(() => setState('idle'), 2000);
    return () => clearTimeout(timer);
  }, [state]);

  const title = state === 'copied' ? 'Kopiert' : state === 'failed' ? 'Kopieren nicht möglich' : label;
  return (
    <Tooltip title={title}>
      <IconButton
        size="small"
        aria-label={label}
        onClick={() =>
          copyToClipboard(text).then(
            () => setState('copied'),
            () => setState('failed'),
          )
        }
      >
        {state === 'copied' ? <Check size={16} color="green" /> : <Copy size={16} />}
      </IconButton>
    </Tooltip>
  );
}
