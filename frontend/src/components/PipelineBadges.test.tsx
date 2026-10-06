import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { pod, service } from '../test/fixtures';
import { PipelineBadges } from './PipelineBadges';
import { ServiceDot, hasRunningPipeline } from './ServiceDot';

const pipeline = { action: 'deploy', title: 'Deployen', url: 'https://gitlab.example/p/-/pipelines/1', startedAt: '2026-10-06T14:00:00Z' };

describe('PipelineBadges', () => {
  it('zeigt je laufender Pipeline einen Link nach GitLab', () => {
    render(<PipelineBadges pipelines={[pipeline]} />);
    const link = screen.getByRole('link', { name: 'Pipeline Deployen läuft – in GitLab öffnen' });
    expect(link).toHaveAttribute('href', pipeline.url);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveTextContent('Pipeline läuft');
  });

  it('rendert nichts ohne Pipelines', () => {
    const { container } = render(<PipelineBadges pipelines={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('ServiceDot', () => {
  it('markiert laufende Pipelines am Service oder an einem Replica', () => {
    const plain = service('web', true, [pod('web-1', true)]);
    expect(hasRunningPipeline(plain)).toBe(false);
    expect(hasRunningPipeline({ ...plain, pipelines: [pipeline] })).toBe(true);
    const podWithPipeline = { ...pod('web-1', true), pipelines: [pipeline] };
    const marked = service('web', true, [podWithPipeline]);
    expect(hasRunningPipeline(marked)).toBe(true);

    const { container } = render(<ServiceDot service={marked} />);
    expect(container.querySelector('[data-pipeline="true"]')).not.toBeNull();
  });
});
