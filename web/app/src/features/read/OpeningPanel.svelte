<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
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
  <p
    class="pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_70%_68%_at_50%_50%,transparent_42%,black_100%)] px-6 pt-6 text-caption break-all text-ink-soft/15 tabular-nums"
  >
    {page.noise}
  </p>
  <div
    class="pointer-events-none absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-300/6 to-transparent"
  ></div>
  <div
    class="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"
  ></div>
  <div
    class="pointer-events-none absolute inset-0 bg-[radial-gradient(40%_46%_at_50%_40%,oklch(0.67_0.16_252/0.11),oklch(0.72_0.14_305/0.05)_55%,transparent_78%)]"
  ></div>
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
