<script lang="ts">
  import { BUTTON_BASE, BUTTON_SIZES, BUTTON_VARIANTS } from '../../shared/ui/button/button-classes.ts';
  import { RANGES } from '../api/types.ts';
  import type { Range } from '../api/types.ts';
  import { getDashboardTools } from './context.ts';

  interface Props {
    readonly range: Range;
    readonly onrange: (range: Range) => void;
  }

  const POSITIONS: Readonly<Record<Range, string>> = {
    '24h': 'translate-x-0',
    '7d': 'translate-x-full',
    '30d': 'translate-x-[200%]',
    '90d': 'translate-x-[300%]',
    '12m': 'translate-x-[400%]',
  };

  const { range, onrange }: Props = $props();
  const { translate } = getDashboardTools();
  const position = $derived(POSITIONS[range]);
</script>

<div
  role="group"
  aria-label={translate('dashboard.range.label')}
  class="relative grid w-full grid-cols-5 rounded-[calc(var(--radius-control)+4px)] p-1 well sm:w-auto"
>
  <span
    class={[
      'pointer-events-none absolute inset-y-1 left-1 w-[calc((100%-0.5rem)/5)] transition-transform duration-(--duration-spring) ease-spring',
      position,
    ]}
    aria-hidden="true"
  >
    <span class={['block size-full rounded-control', BUTTON_VARIANTS.tint]}></span>
  </span>
  {#each RANGES as option (option)}
    <button
      type="button"
      aria-pressed={option === range}
      class={[
        BUTTON_BASE,
        BUTTON_SIZES.compact,
        'relative z-10 px-2 tabular-nums transition-colors sm:px-3.5',
        option === range ? 'text-ink' : 'text-ink-mute hover:text-ink',
      ]}
      onclick={() => onrange(option)}
    >
      {translate(`dashboard.range.${option}`)}
    </button>
  {/each}
</div>
