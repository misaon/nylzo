<script lang="ts">
  import type { Overview } from '../../api/types.ts';
  import BarList from '../../charts/BarList.svelte';
  import { getDashboardTools } from '../context.ts';
  import { pageName, robotName } from '../names.ts';
  import Skeleton from '../Skeleton.svelte';
  import { CARD, NOTE } from '../surfaces.ts';

  interface Props {
    readonly overview: Overview;
    readonly loading: boolean;
  }

  const { overview, loading }: Props = $props();
  const { translate, format } = getDashboardTools();
  const pages = $derived(
    overview.pages
      .filter((page) => page.value > 0)
      .map((page) => ({ key: page.name, label: pageName(translate, page.name), value: page.value })),
  );
  const robots = $derived(
    overview.robots
      .filter((robot) => robot.value > 0)
      .map((robot) => ({ key: robot.name, label: robotName(translate, robot.name), value: robot.value })),
  );
</script>

<div class="grid gap-4 lg:grid-cols-2">
  <section aria-labelledby="dashboard-pages" class={CARD}>
    <h3 id="dashboard-pages" class="text-title-sm text-ink">{translate('dashboard.pages.title')}</h3>
    <p class={[NOTE, 'mt-0.5']}>{translate('dashboard.pages.list')}</p>
    <div class="-mx-3 mt-3">
      {#if loading}
        <Skeleton rows={5} class="px-3 py-3" />
      {:else if pages.length > 0}
        <BarList
          label={translate('dashboard.pages.list')}
          items={pages}
          tone="people"
          formatValue={format.count}
          formatShare={format.percent}
        />
      {:else}
        <p class="px-3 py-6 text-caption text-ink-mute">{translate('dashboard.pages.empty')}</p>
      {/if}
    </div>
  </section>
  <section aria-labelledby="dashboard-robots" class={CARD}>
    <h3 id="dashboard-robots" class="text-title-sm text-ink">{translate('dashboard.traffic.robots.title')}</h3>
    <p class={[NOTE, 'mt-0.5']}>{translate('dashboard.traffic.robots.note')}</p>
    <div class="-mx-3 mt-3">
      {#if loading}
        <Skeleton rows={5} class="px-3 py-3" />
      {:else if robots.length > 0}
        <BarList
          label={translate('dashboard.traffic.robots.title')}
          items={robots}
          tone="robots"
          formatValue={format.count}
          formatShare={format.percent}
        />
      {:else}
        <p class="px-3 py-6 text-caption text-ink-mute">{translate('dashboard.traffic.robots.empty')}</p>
      {/if}
    </div>
  </section>
</div>
