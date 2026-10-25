<script lang="ts">
  import type { PipelineNode } from './pipeline.ts';

  import Icon from '../ui/Icon.svelte';

  interface Props {
    readonly nodes: readonly PipelineNode[];
    readonly current: number;
    readonly progress: number;
    readonly active: boolean;
    readonly size?: 'narrow' | 'wide';
  }

  type StepState = 'current' | 'done' | 'pending';

  const LAYOUT = {
    wide: { gap: 'gap-5 sm:gap-6', width: 'max-w-[560px]' },
    narrow: { gap: 'gap-7 sm:gap-8', width: 'max-w-[480px]' },
  } as const;
  const COLUMNS: Readonly<Record<number, string>> = { 4: 'grid-cols-4', 5: 'grid-cols-5' };
  const INSETS: Readonly<Record<number, string>> = { 4: 'inset-x-[12.5%]', 5: 'inset-x-[10%]' };
  const ICON_TONES: Readonly<Record<StepState, string>> = {
    current: 'text-white',
    done: 'text-aegis-200',
    pending: 'text-ink-mute',
  };
  const LABEL_TONES: Readonly<Record<StepState, string>> = {
    current: 'text-aegis-200',
    done: 'text-ink-soft',
    pending: 'text-ink-mute',
  };

  const { nodes, current, progress, active, size = 'wide' }: Props = $props();

  const columns = $derived(COLUMNS[nodes.length] ?? 'grid-cols-3');
  const inset = $derived(INSETS[nodes.length] ?? 'inset-x-[16.666%]');
  const segments = $derived(Math.max(1, nodes.length - 1));
  const track = $derived(Math.min(1, Math.max(0, (progress * nodes.length) / segments)));

  function stateOf(index: number): StepState {
    if (active && index === current) {
      return 'current';
    }
    return index < current || (!active && current >= nodes.length) ? 'done' : 'pending';
  }
</script>

<div class={['flex w-full flex-col items-center', LAYOUT[size].gap]}>
  <div class={['relative w-full', LAYOUT[size].width]} aria-hidden="true">
    <span class={['absolute top-5 h-px bg-white/10 md:top-[22px]', inset]}></span>
    <span class={['absolute top-5 h-px md:top-[22px]', inset]}>
      <progress class="block h-px w-full progress-line" max="1" value={track}></progress>
    </span>
    <ol class={['relative grid w-full', columns]}>
      {#each nodes as node, index (node.id)}
        {const state = $derived(stateOf(index))}
        <li class="relative flex flex-col items-center gap-2">
          <span
            class={[
              'relative grid size-10 place-items-center rounded-control transition-transform duration-500 ease-silk md:size-11',
              state === 'current' ? 'scale-110' : 'scale-100',
            ]}
          >
            <span class="absolute inset-0 rounded-control bg-void-900"></span>
            <span class="absolute inset-0 rounded-control bg-white/3 ring-1 ring-white/10"></span>
            <span
              class={[
                'absolute inset-0 rounded-control bg-aegis-500/15 ring-1 ring-aegis-400/40 transition-opacity duration-500 ease-silk',
                state === 'done' ? 'opacity-100' : 'opacity-0',
              ]}
            ></span>
            <span
              class={[
                'absolute inset-0 rounded-control bg-aegis-500/30 shadow-[0_0_30px_-4px_oklch(0.56_0.19_258/0.95)] ring-1 ring-aegis-300/70 transition-opacity duration-500 ease-silk',
                state === 'current' ? 'opacity-100' : 'opacity-0',
              ]}
            ></span>
            <span
              class={[
                'absolute -inset-1.5 transition-opacity duration-500',
                state === 'current' ? 'opacity-100' : 'opacity-0',
              ]}
            >
              <span class="absolute inset-0 animate-pulse-core rounded-[15px] ring-1 ring-aegis-300/45"></span>
            </span>
            <Icon name={node.icon} stroke={1.7} class={['relative size-[18px] md:size-5', ICON_TONES[state]]} />
          </span>
          <span class={['text-[11px] md:text-xs', LABEL_TONES[state]]}>{node.label}</span>
        </li>
      {/each}
    </ol>
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
        <p class="text-[15px] text-ink">{node.title}</p>
        <p class="mt-1 text-[13px] text-pretty text-ink-mute">{node.detail}</p>
      </div>
    {/each}
  </div>
</div>
