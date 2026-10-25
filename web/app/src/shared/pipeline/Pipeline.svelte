<script lang="ts">
  import type { PipelineNode } from './pipeline.ts';
  import Icon from '../ui/Icon.svelte';

  interface Props {
    readonly nodes: readonly PipelineNode[];
    readonly current: number;
    readonly progress: number;
    readonly active: boolean;
    readonly size?: 'wide' | 'narrow';
  }

  const LAYOUT = {
    wide: { gap: 'gap-5 sm:gap-6', width: 'max-w-[560px]' },
    narrow: { gap: 'gap-7 sm:gap-8', width: 'max-w-[480px]' },
  } as const;

  const { nodes, current, progress, active, size = 'wide' }: Props = $props();

  const columns = $derived(nodes.length === 5 ? 'grid-cols-5' : nodes.length === 4 ? 'grid-cols-4' : 'grid-cols-3');
  const inset = $derived(
    nodes.length === 5 ? 'inset-x-[10%]' : nodes.length === 4 ? 'inset-x-[12.5%]' : 'inset-x-[16.666%]',
  );
  const track = $derived(Math.min(1, Math.max(0, (progress * nodes.length) / Math.max(1, nodes.length - 1))));
</script>

<div class={['flex w-full flex-col items-center', LAYOUT[size].gap]}>
  <div class={['relative w-full', LAYOUT[size].width]} aria-hidden="true">
    <span class={['absolute top-5 h-px bg-white/10 md:top-[22px]', inset]}></span>
    <span class={['absolute top-5 h-px md:top-[22px]', inset]}>
      <progress class="block h-px w-full progress-line" max="1" value={track}></progress>
    </span>
    <ol class={['relative grid w-full', columns]}>
      {#each nodes as node, index (node.id)}
        {@const isCurrent = active && index === current}
        {@const isDone = index < current || (!active && current >= nodes.length)}
        <li class="relative flex flex-col items-center gap-2">
          <span
            class={[
              'relative grid size-10 place-items-center rounded-control transition-transform duration-500 ease-silk md:size-11',
              isCurrent ? 'scale-110' : 'scale-100',
            ]}
          >
            <span class="absolute inset-0 rounded-control bg-void-900"></span>
            <span class="absolute inset-0 rounded-control bg-white/3 ring-1 ring-white/10"></span>
            <span
              class={[
                'absolute inset-0 rounded-control bg-aegis-500/15 ring-1 ring-aegis-400/40 transition-opacity duration-500 ease-silk',
                isDone ? 'opacity-100' : 'opacity-0',
              ]}
            ></span>
            <span
              class={[
                'absolute inset-0 rounded-control bg-aegis-500/30 shadow-[0_0_30px_-4px_oklch(0.56_0.19_258/0.95)] ring-1 ring-aegis-300/70 transition-opacity duration-500 ease-silk',
                isCurrent ? 'opacity-100' : 'opacity-0',
              ]}
            ></span>
            <span
              class={['absolute -inset-1.5 transition-opacity duration-500', isCurrent ? 'opacity-100' : 'opacity-0']}
            >
              <span class="absolute inset-0 animate-pulse-core rounded-[15px] ring-1 ring-aegis-300/45"></span>
            </span>
            <Icon
              name={node.icon}
              stroke={1.7}
              class={[
                'relative size-[18px] md:size-5',
                isCurrent ? 'text-white' : isDone ? 'text-aegis-200' : 'text-ink-mute',
              ]}
            />
          </span>
          <span
            class={[
              'text-[11px] md:text-xs',
              isCurrent ? 'text-aegis-200' : isDone ? 'text-ink-soft' : 'text-ink-mute',
            ]}>{node.label}</span
          >
        </li>
      {/each}
    </ol>
  </div>
  <div class="stack w-full max-w-[460px] text-center" aria-hidden="true">
    {#each nodes as node, index (node.id)}
      {@const shown = active && index === current}
      <div
        class={[
          'transition-[opacity,translate]',
          shown
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
