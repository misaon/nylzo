<script lang="ts">
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../shared/ui/icon/icons.ts';

  interface Props {
    readonly icon: IconName;
    readonly text: string;
  }

  const { icon, text }: Props = $props();
</script>

<p
  class="flex items-center justify-center gap-2 text-center text-caption text-ink-mute max-sm:block max-sm:text-balance"
>
  <Icon
    name={icon}
    class="size-4 shrink-0 text-aegis-300 max-sm:mr-1.5 max-sm:inline-block max-sm:align-[-3px]"
  />{text}
</p>
