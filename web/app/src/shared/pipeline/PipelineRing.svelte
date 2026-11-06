<script lang="ts">
  import { getTranslator } from '../language/translator.svelte.ts';
  import Icon from '../ui/icon/Icon.svelte';
  import type { PipelineNode } from './pipeline.ts';

  interface Props {
    readonly nodes: readonly PipelineNode[];
    readonly current: number;
    readonly progress: number;
    readonly active: boolean;
  }

  const STEP_GAP = 2.4;

  const { nodes, current, progress, active }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();
  const segment = $derived(100 / Math.max(1, nodes.length));
  const shown = $derived(Math.min(current, nodes.length - 1));
</script>

<div class="flex w-full flex-col items-center gap-4 md:gap-5">
  <div class="relative grid size-24 shrink-0 place-items-center md:size-28" aria-hidden="true">
    <span
      class="absolute -inset-6 rounded-full bg-[radial-gradient(closest-side,oklch(0.67_0.16_252/0.26),oklch(0.72_0.14_305/0.08)_62%,transparent)] motion-safe:animate-pulse-core"
    ></span>
    <svg class="absolute inset-0 size-full -rotate-90 overflow-visible" viewBox="0 0 120 120" fill="none">
      <defs>
        <linearGradient id="{id}-tone" x1="0" y1="0" x2="1" y2="1">
          <stop stop-color="#98D5FF"></stop>
          <stop offset=".52" stop-color="#7AA7F4"></stop>
          <stop offset="1" stop-color="#B4A0F2"></stop>
        </linearGradient>
        <mask id="{id}-steps">
          <circle
            cx="60"
            cy="60"
            r="54"
            stroke="#fff"
            stroke-width="6"
            pathLength="100"
            stroke-dasharray="{segment - STEP_GAP} {STEP_GAP}"
            stroke-dashoffset={-STEP_GAP / 2}
          ></circle>
        </mask>
      </defs>
      <g mask="url(#{id}-steps)">
        <circle cx="60" cy="60" r="54" stroke="oklch(1 0 0 / 0.1)" stroke-width="3"></circle>
        <circle
          cx="60"
          cy="60"
          r="54"
          stroke="url(#{id}-tone)"
          stroke-width="3"
          stroke-linecap="round"
          pathLength="100"
          stroke-dasharray="{progress * 100} 100"
          class="transition-[stroke-dasharray] duration-500 ease-silk"
        ></circle>
      </g>
      <g transform="rotate({progress * 360} 60 60)">
        <circle
          cx="114"
          cy="60"
          r="5"
          class="origin-center fill-white drop-shadow-[0_0_8px_oklch(0.86_0.06_246/0.9)] [transform-box:fill-box] motion-safe:animate-pulse-core md:[r:4.3px]"
        ></circle>
      </g>
    </svg>
    <span class="relative grid size-[58px] place-items-center rounded-full step-current md:size-[66px]">
      <span class="absolute -inset-1.5 rounded-full ring-1 ring-aegis-300/25"></span>
      <span class="grid">
        {#each nodes as node, index (node.id)}
          <Icon
            name={node.icon}
            class={[
              'col-start-1 row-start-1 size-6 text-white transition-[opacity,scale] duration-500 ease-silk',
              index === shown ? 'scale-100 opacity-100' : 'scale-75 opacity-0',
            ]}
          />
        {/each}
      </span>
    </span>
  </div>
  <div class="stack w-full max-w-[460px] text-center" aria-hidden="true">
    {#each nodes as node, index (node.id)}
      <div
        class={[
          'transition-[opacity,translate]',
          active && index === current
            ? 'translate-y-0 opacity-100 delay-[120ms] duration-500 ease-silk'
            : 'translate-y-1.5 opacity-0 duration-150 ease-in',
        ]}
      >
        <p class="mb-1.5 text-eyebrow font-medium text-ink-mute uppercase">
          {translate('step.counter', { step: index + 1, total: nodes.length })}
        </p>
        <p class="text-title font-medium text-ink">{node.title}</p>
        <p class="mt-1.5 text-body-sm text-pretty text-ink-soft">{node.detail}</p>
      </div>
    {/each}
  </div>
</div>
