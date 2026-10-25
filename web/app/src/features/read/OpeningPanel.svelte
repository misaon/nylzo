<script lang="ts">
  import type { PipelineNode } from '../../shared/pipeline/pipeline.ts';
  import type { ReadPage } from './read-page.svelte.ts';

  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Pipeline from '../../shared/pipeline/Pipeline.svelte';
  import { PANEL_HIDDEN, PANEL_SHOWN } from './read-page.svelte.ts';

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

<div
  class={['relative overflow-hidden fade-swap', page.phase === 'opening' ? PANEL_SHOWN : PANEL_HIDDEN]}
  aria-hidden="true"
>
  <p
    class="pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_62%_58%_at_50%_50%,transparent_35%,black_100%)] px-6 pt-6 font-mono text-[13px] leading-6 break-all text-aegis-200/30"
  >
    {page.noise}
  </p>
  <div
    class="pointer-events-none absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-400/12 to-transparent"
  ></div>
  <div
    class="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"
  ></div>
  <div class="relative flex h-full flex-col items-center justify-center px-4 py-12">
    <Pipeline
      {nodes}
      size="narrow"
      current={page.timeline.paced.step}
      progress={page.timeline.paced.progress}
      active={page.phase === 'opening'}
    />
  </div>
</div>
