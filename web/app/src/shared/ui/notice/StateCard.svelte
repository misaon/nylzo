<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly loading?: string | undefined;
    readonly class?: ClassValue;
    readonly children?: Snippet;
  }

  const { loading, class: className, children }: Props = $props();
</script>

<div
  class={[
    'flex flex-col items-center rounded-pane bg-white/4 px-6 py-12 text-center shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] ring-1 ring-white/8 md:py-14',
    className,
  ]}
>
  {#if loading === undefined}
    {@render children?.()}
  {:else}
    <span class="grid size-14 place-items-center text-aegis-300" aria-hidden="true">
      <svg
        class="size-8 motion-safe:animate-spin"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
      >
        <circle cx="12" cy="12" r="9" stroke-opacity="0.2"></circle>
        <path d="M21 12a9 9 0 0 0-9-9"></path>
      </svg>
    </span>
    <p role="status" class="mt-5 text-body text-ink-soft">{loading}</p>
  {/if}
</div>
