<script lang="ts">
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { Range } from '../api/types.ts';
  import { getDashboardTools } from './context.ts';

  interface Props {
    readonly change: number | undefined;
    readonly range: Range;
    readonly growthIsGood?: boolean;
  }

  const { change, range, growthIsGood = true }: Props = $props();
  const { translate, format } = getDashboardTools();
  const rounded = $derived(change === undefined ? undefined : Math.round(change * 100));
  const tone = $derived.by(() => {
    if (rounded === undefined || rounded === 0 || !growthIsGood) {
      return 'text-ink-soft';
    }
    return rounded > 0 ? 'text-aegis-200' : 'text-rose-200';
  });
</script>

{#if rounded !== undefined}
  <p class="flex min-w-0 items-center gap-x-1.5 text-caption whitespace-nowrap">
    <span class={['inline-flex items-center gap-1 font-medium tabular-nums', tone]}>
      {#if rounded !== 0}
        <Icon name="arrowRight" class={['size-3.5', rounded > 0 ? '-rotate-45' : 'rotate-45']} stroke={2.2} />
      {/if}
      {format.change(rounded / 100)}
    </span>
    <span class="truncate text-ink-mute" title={translate(`dashboard.trend.${range}`)}>
      {translate(`dashboard.trend.${range}`)}
    </span>
  </p>
{/if}
