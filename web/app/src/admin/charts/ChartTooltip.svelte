<script lang="ts">
  import { SWATCHES } from './tones.ts';
  import type { TooltipRow } from './tones.ts';

  interface Props {
    readonly x: number;
    readonly top: number;
    readonly bottom: number;
    readonly left: number;
    readonly right: number;
    readonly title: string;
    readonly rows: readonly TooltipRow[];
  }

  const BOX_WIDTH = 224;
  const OFFSET = 14;

  const { x, top, bottom, left, right, title, rows }: Props = $props();
  const flipped = $derived(x + OFFSET + BOX_WIDTH > right);
  const boxX = $derived(Math.max(left - OFFSET, flipped ? x - OFFSET - BOX_WIDTH : x + OFFSET));
</script>

<foreignObject x={boxX} y={top} width={BOX_WIDTH} height={Math.max(0, bottom - top)} class="pointer-events-none">
  <div class={['flex', flipped ? 'justify-end' : 'justify-start']}>
    <div class="min-w-36 rounded-pane bg-void-900/95 px-3 py-2.5 text-caption shadow-rim ring-1 ring-white/12">
      <p class="font-medium whitespace-nowrap text-ink">{title}</p>
      <ul role="list" class="mt-1.5 grid gap-1">
        {#each rows as row (row.label)}
          <li class="flex items-center gap-2 whitespace-nowrap text-ink-soft">
            <span class={['size-2 shrink-0 rounded-full', SWATCHES[row.tone]]}></span>
            <span class="flex-1">{row.label}</span>
            <span class="pl-3 font-medium text-ink tabular-nums">{row.value}</span>
          </li>
        {/each}
      </ul>
    </div>
  </div>
</foreignObject>
