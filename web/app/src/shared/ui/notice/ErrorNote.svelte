<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';

  interface Props {
    readonly id?: string | undefined;
    readonly message: string;
    readonly class?: ClassValue | undefined;
  }

  const { id, message, class: className }: Props = $props();
</script>

<p {id} role="alert" class={['inline-flex items-center gap-2 text-caption font-medium text-rose-200', className]}>
  <span
    class="grid size-4.5 shrink-0 place-items-center rounded-full bg-rose-400 text-void-950 shadow-[0_0_12px_-2px_oklch(0.65_0.2_15/0.7)]"
    aria-hidden="true"
  >
    <Icon name="exclamation" class="size-3" stroke={3} />
  </span>
  <span class="text-pretty">{message}</span>
</p>
