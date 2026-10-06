import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CopyCommandButtons } from './CopyCommandButtons';

afterEach(() => vi.unstubAllGlobals());

describe('CopyCommandButtons', () => {
  it('kopiert den fertigen Befehl in die Zwischenablage und bestätigt das', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('isSecureContext', true);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });

    render(
      <CopyCommandButtons
        commands={[{ id: 'shell', title: 'Shell', lucideIcon: 'terminal', lucideIconColor: '', command: 'kubectl exec -it web -- sh' }]}
      />,
    );
    const button = screen.getByRole('button', { name: 'Shell kopieren' });
    await user.hover(button);
    expect(await screen.findByText('kubectl exec -it web -- sh')).toBeInTheDocument();

    await user.click(button);
    expect(writeText).toHaveBeenCalledWith('kubectl exec -it web -- sh');
    await waitFor(() => expect(screen.getByRole('tooltip')).toHaveTextContent('Kopiert'));
  });

  it('rendert nichts ohne Befehle', () => {
    const { container } = render(<CopyCommandButtons commands={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
