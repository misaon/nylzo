<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import CipherNoise from '../../shared/pipeline/CipherNoise.svelte';
  import type { PipelineNode } from '../../shared/pipeline/pipeline.ts';
  import PipelineRing from '../../shared/pipeline/PipelineRing.svelte';
  import type { ReadPage } from './read-page.svelte.ts';
  import { panelClasses } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();

  const nodes: readonly PipelineNode[] = $derived(
    page.steps.map(({ id, icon }) => ({
      id,
      label: translate(`open.step.${id}.label`),
      icon,
      title: translate(`open.step.${id}.title`),
      detail: translate(`open.step.${id}.detail`),
    })),
  );
</script>

<div class={['overflow-hidden fade-swap', panelClasses(page.phase === 'opening', page.phase)]} aria-hidden="true">
  <CipherNoise text={page.noise} layerClass="pointer-events-none" />
  <div class="relative flex h-full flex-col items-center justify-center px-4 py-12">
    <PipelineRing
      {nodes}
      size="large"
      current={page.timeline.paced.step}
      progress={page.timeline.paced.progress}
      active={page.phase === 'opening'}
    />
  </div>
</div>
