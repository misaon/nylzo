<script lang="ts">
  import type { Live, Overview } from '../../api/types.ts';
  import Track from '../../charts/Track.svelte';
  import { getDashboardTools } from '../context.ts';
  import Section from '../Section.svelte';
  import { NOTE, TILE } from '../surfaces.ts';
  import { usageValues } from './usage.ts';
  import UsageChart from './UsageChart.svelte';

  interface Props {
    readonly overview: Overview;
    readonly live: Live;
    readonly loading: boolean;
    readonly waiting: boolean;
  }

  const { overview, live, loading, waiting }: Props = $props();
  const { translate, format } = getDashboardTools();
  const latest = $derived(live.usage.at(-1));
  const processor = $derived(
    usageValues(live.usageSupported, live.usage, (point) => (point.processor >= 0 ? point.processor : undefined)),
  );
  const memory = $derived(
    usageValues(live.usageSupported, live.usage, (point) =>
      point.memoryTotal > 0 ? point.memoryUsed / point.memoryTotal : undefined,
    ),
  );
  const storage = $derived(overview.storage);
  const diskKnown = $derived(storage.totalBytes > 0);
  const unknown = $derived(translate('dashboard.unknown'));
</script>

{#snippet value(pending: boolean, text: string)}
  {#if pending}
    <span class="inline-block h-5 w-20 animate-pulse rounded-xs bg-white/8 align-middle"></span>
  {:else}
    {text}
  {/if}
{/snippet}

<Section id="dashboard-server" icon="server" title={translate('dashboard.server.title')}>
  <div class="grid gap-4 lg:grid-cols-2">
    <UsageChart
      title={translate('dashboard.server.processor')}
      label={translate('dashboard.server.chart.processor')}
      usage={live.usage}
      values={processor}
      {waiting}
      tone="people"
      current={processor === undefined ? '–' : format.percent(processor.at(-1) ?? 0)}
      detail={latest && latest.load >= 0
        ? translate('dashboard.server.load', { load: format.decimal(latest.load) })
        : translate('dashboard.now.five')}
    />
    <UsageChart
      title={translate('dashboard.server.memory')}
      label={translate('dashboard.server.chart.memory')}
      usage={live.usage}
      values={memory}
      {waiting}
      tone="robots"
      current={memory === undefined ? '–' : format.percent(memory.at(-1) ?? 0)}
      detail={memory === undefined || !latest
        ? translate('dashboard.now.five')
        : translate('dashboard.now.memory.value', {
            used: format.bytes(latest.memoryUsed),
            total: format.bytes(latest.memoryTotal),
          })}
    />
  </div>
  <dl class="grid grid-cols-2 gap-3 lg:grid-cols-4">
    <div class={[TILE, 'px-4 py-3.5']}>
      <dt class="text-caption text-ink-soft">{translate('dashboard.server.process')}</dt>
      <dd class="mt-1 text-title font-medium text-ink tabular-nums">
        {@render value(waiting, latest && latest.processBytes > 0 ? format.bytes(latest.processBytes) : unknown)}
      </dd>
    </div>
    <div class={[TILE, 'px-4 py-3.5']}>
      <dt class="text-caption text-ink-soft">{translate('dashboard.server.disk')}</dt>
      <dd class="mt-1">
        <span class="block text-title font-medium text-ink tabular-nums">
          {@render value(loading, diskKnown ? format.bytes(storage.freeBytes) : unknown)}
          {#if diskKnown}
            <span class="text-caption font-normal text-ink-mute">
              {translate('dashboard.now.disk.value', { total: format.bytes(storage.totalBytes) })}
            </span>
          {/if}
        </span>
        {#if diskKnown || loading}
          <Track
            class="mt-2.5"
            value={diskKnown ? 1 - storage.freeBytes / storage.totalBytes : 0}
            reserve={diskKnown ? storage.reserveBytes / storage.totalBytes : undefined}
            tone="people"
          />
          <span class={[NOTE, 'mt-2 block min-h-10']}>
            {#if diskKnown}
              {translate('dashboard.now.disk.reserve', { reserve: format.bytes(storage.reserveBytes) })}
            {/if}
          </span>
        {/if}
      </dd>
    </div>
    <div class={[TILE, 'px-4 py-3.5']}>
      <dt class="text-caption text-ink-soft">{translate('dashboard.server.database')}</dt>
      <dd class="mt-1 text-title font-medium text-ink tabular-nums">
        {@render value(loading, storage.databaseBytes > 0 ? format.bytes(storage.databaseBytes) : unknown)}
      </dd>
    </div>
    <div class={[TILE, 'px-4 py-3.5']}>
      <dt class="text-caption text-ink-soft">{translate('dashboard.server.version')}</dt>
      <dd class="mt-1 text-title font-medium wrap-anywhere text-ink tabular-nums">
        {@render value(loading, overview.version || unknown)}
      </dd>
    </div>
  </dl>
</Section>
