<script lang="ts">
  import type { Snippet } from 'svelte';

  import type { IconName } from '../../shared/ui/icon/icons.ts';
  import IconTile from '../../shared/ui/tile/IconTile.svelte';

  interface Props {
    readonly id: string;
    readonly icon: IconName;
    readonly title: string;
    readonly children: Snippet;
  }

  const { id, icon, title, children }: Props = $props();
</script>

<section aria-labelledby={id} class="grid gap-4">
  <div class="flex items-center gap-3">
    <IconTile {icon} tone="current" size="small" />
    <h2 {id} class="text-title text-ink">{title}</h2>
  </div>
  {@render children()}
</section>
