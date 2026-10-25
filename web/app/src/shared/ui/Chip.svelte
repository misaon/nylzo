<script lang="ts">
  import type { Snippet } from 'svelte';

  import type { IconName } from './icons.ts';
  import Icon from './Icon.svelte';

  interface Props {
    readonly icon: IconName;
    readonly accent?: boolean;
    readonly children: Snippet;
  }

  const { icon, accent = false, children }: Props = $props();
</script>

<li
  class={[
    'flex h-9 items-center gap-2 rounded-[9px] px-3 text-[13px] whitespace-nowrap ring-1',
    accent ? 'bg-nebula-400/10 text-nebula-300 ring-nebula-300/30' : 'text-ink-soft ring-white/8',
  ]}
>
  <Icon name={icon} class={['size-4 shrink-0', !accent && 'text-ink-mute']} />
  {@render children()}
</li>
