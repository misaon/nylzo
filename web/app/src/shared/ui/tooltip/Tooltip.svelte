<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';
  import type { IconName } from '../icon/icons.ts';

  interface Props {
    readonly label: string;
    readonly title: string;
    readonly text: string;
    readonly icon?: IconName;
    readonly class?: ClassValue | undefined;
  }

  const { label, title, text, icon = 'alert', class: className }: Props = $props();
  const id = $props.id();
  let pinned = $state(false);
</script>

<span class="group/tip relative inline-flex">
  <button
    type="button"
    aria-describedby="{id}-tip"
    aria-expanded={pinned}
    onclick={() => (pinned = !pinned)}
    onkeydown={(event) => {
      if (event.key === 'Escape') {
        pinned = false;
      }
    }}
    onblur={() => (pinned = false)}
    class={[
      'inline-flex items-center gap-1.5 underline decoration-white/30 decoration-dotted underline-offset-4 transition hover:text-ink-soft max-sm:-my-3',
      className,
    ]}
  >
    <Icon name={icon} class="size-3.5 shrink-0" stroke={2} />
    {label}
  </button>
  <span
    role="tooltip"
    id="{id}-tip"
    class={[
      'pointer-events-none absolute bottom-full left-0 z-30 mb-2 w-max max-w-[min(17rem,calc(100vw-3rem))] rounded-pane px-3.5 py-2.5 text-left text-caption text-pretty text-ink-soft glass-pop transition-[opacity,translate] duration-200',
      pinned
        ? 'translate-y-0 opacity-100'
        : 'translate-y-1 opacity-0 group-hover/tip:translate-y-0 group-hover/tip:opacity-100 group-has-focus-visible/tip:translate-y-0 group-has-focus-visible/tip:opacity-100',
    ]}
  >
    <span class="mb-0.5 block font-medium text-ink">{title}</span>{text}
  </span>
</span>
