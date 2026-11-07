<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';

  interface Props {
    readonly id?: string;
    readonly message: string;
    readonly class?: ClassValue | undefined;
    readonly action?: Snippet;
  }

  const { id, message, class: className, action }: Props = $props();
</script>

<div
  class={[
    'flex items-center gap-3 rounded-card bg-[oklch(0.66_0.13_75/0.1)] p-3 text-left ring-1 ring-[oklch(0.82_0.12_80/0.32)] sm:gap-3.5 sm:p-3.5',
    className,
  ]}
>
  <span class="grid size-9 shrink-0 place-items-center rounded-control text-white tile-warning" aria-hidden="true">
    <Icon name="caution" class="size-4.5" stroke={1.9} />
  </span>
  <p {id} role="alert" class="min-w-0 flex-1 text-body-sm font-medium text-pretty text-ink">{message}</p>
  {@render action?.()}
</div>
