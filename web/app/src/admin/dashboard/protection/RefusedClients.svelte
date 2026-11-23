<script lang="ts">
  import { flip } from 'svelte/animate';

  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { motionMs } from '../../../shared/ui/transition/motion.ts';
  import type { RefusedClient } from '../../api/types.ts';
  import { getDashboardTools } from '../context.ts';
  import { countryName, limitName } from '../names.ts';
  import Skeleton from '../Skeleton.svelte';
  import { NOTE, TILE } from '../surfaces.ts';

  interface Props {
    readonly clients: readonly RefusedClient[];
    readonly waiting: boolean;
    readonly now: number;
  }

  const REORDER_MS = 400;
  const COLUMNS =
    'md:grid md:grid-cols-[minmax(0,1.3fr)_minmax(0,0.9fr)_minmax(0,1.6fr)_4rem_minmax(0,1.1fr)] md:gap-4';

  const { clients, waiting, now }: Props = $props();
  const { translate, format } = getDashboardTools();
  const columns = $derived([
    translate('dashboard.protection.client'),
    translate('dashboard.protection.country'),
    translate('dashboard.protection.reasons'),
    translate('dashboard.protection.count'),
    `${translate('dashboard.protection.first')} / ${translate('dashboard.protection.last')}`,
  ]);

  function reasons(client: RefusedClient): { readonly key: string; readonly label: string; readonly count: number }[] {
    return Object.entries(client.reasons)
      .filter(([, count]) => count > 0)
      .toSorted(([, first], [, second]) => second - first)
      .map(([key, count]) => ({ key, label: limitName(translate, key), count }));
  }
</script>

<div class={[TILE, 'p-4 sm:p-5']}>
  <h3 class="text-title-sm text-ink">{translate('dashboard.protection.clients')}</h3>
  {#if waiting}
    <Skeleton rows={2} class="mt-4" />
  {:else if clients.length === 0}
    <p
      class="mt-4 flex items-center gap-2.5 rounded-pane bg-aegis-500/6 px-4 py-3.5 text-ui text-ink-soft ring-1 ring-aegis-400/20"
    >
      <Icon name="shieldCheck" class="size-4 shrink-0 text-aegis-300" />
      <span>{translate('dashboard.protection.empty')}</span>
    </p>
  {:else}
    <div class={[COLUMNS, 'mt-4 hidden border-b border-white/8 pb-2 text-micro text-ink-mute']} aria-hidden="true">
      {#each columns as column, i (column)}
        <span class={i === 3 ? 'text-right' : undefined}>{column}</span>
      {/each}
    </div>
    <ul role="list" class="mt-2 md:mt-0 md:max-h-[30rem] md:overflow-y-auto">
      {#each clients as client (client.client)}
        <li
          class={[COLUMNS, 'grid gap-2 border-b border-white/6 py-3.5 last:border-b-0 md:items-center']}
          animate:flip={{ duration: motionMs(REORDER_MS) }}
        >
          <div class="flex items-baseline justify-between gap-3 md:block">
            <span class="sr-only md:hidden">{columns[0]}</span>
            <span class="text-ui font-medium wrap-anywhere text-ink tabular-nums" translate="no">{client.client}</span>
            <span class="text-ui font-medium text-ink tabular-nums md:hidden">{format.count(client.count)}</span>
          </div>
          <p class="text-caption text-ink-soft md:text-ui">
            <span class="sr-only">{`${columns[1]}:`}</span>
            {countryName(translate, format, client.country)}
          </p>
          <ul role="list" class="flex flex-wrap gap-1.5" aria-label={columns[2]}>
            {#each reasons(client) as reason (reason.key)}
              <li
                class="inline-flex items-center gap-1.5 rounded-full bg-rose-500/10 py-0.5 pr-2 pl-2.5 text-micro text-rose-100 ring-1 ring-rose-300/25"
              >
                <span>{reason.label}</span>
                <span class="font-medium text-ink tabular-nums">{format.count(reason.count)}</span>
              </li>
            {/each}
          </ul>
          <p class="hidden text-right text-ui font-medium text-ink tabular-nums md:block">
            <span class="sr-only">{`${columns[3]}:`}</span>
            {format.count(client.count)}
          </p>
          <p class="text-caption text-ink-mute tabular-nums">
            <span class="sr-only">{`${columns[4]}:`}</span>
            <time datetime={new Date(client.first * 1000).toISOString()}>
              {format.relative(Math.min(client.first, now), now)}
            </time>
            →
            <time class="text-ink-soft" datetime={new Date(client.last * 1000).toISOString()}>
              {format.relative(Math.min(client.last, now), now)}
            </time>
          </p>
        </li>
      {/each}
    </ul>
  {/if}
  <p class={[NOTE, 'mt-4 flex items-start gap-2']}>
    <Icon name="lock" class="mt-0.5 size-3.5 shrink-0" />
    {translate('dashboard.protection.memory')}
  </p>
</div>
