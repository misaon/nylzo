<script lang="ts">
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../shared/ui/icon/icons.ts';
  import Sparkline from '../../charts/Sparkline.svelte';
  import type { Tone } from '../../charts/tones.ts';
  import Track from '../../charts/Track.svelte';
  import { NOTE, TILE } from '../surfaces.ts';

  interface Props {
    readonly icon: IconName;
    readonly label: string;
    readonly value: string;
    readonly detail: string;
    readonly share: number | undefined;
    readonly tone: Tone;
    readonly history?: readonly number[] | undefined;
    readonly reserve?: number | undefined;
    // The detail runs to two lines once it is known, so the tile keeps room for them.
    readonly tall?: boolean;
  }

  const { icon, label, value, detail, share, tone, history, reserve, tall = false }: Props = $props();
</script>

<div class={[TILE, 'flex min-w-0 flex-col gap-2.5 p-4']}>
  <div class="flex items-center justify-between gap-3">
    <p class="flex items-center gap-2 text-caption text-ink-soft">
      <Icon name={icon} class="size-4 shrink-0 text-aegis-300" />
      {label}
    </p>
    <p class="text-title font-medium text-ink tabular-nums">{value}</p>
  </div>
  <Track value={share ?? 0} {tone} {reserve} />
  <Sparkline values={history ?? []} {tone} maximum={1} class="h-8" />
  <p class={[NOTE, 'mt-auto', tall && 'min-h-10']}>{detail}</p>
</div>
