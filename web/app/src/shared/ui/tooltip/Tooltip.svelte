<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import { on } from 'svelte/events';

  import Icon from '../icon/Icon.svelte';

  interface Props {
    readonly label: string;
    readonly title: string;
    readonly text: string;
  }

  const { label, title, text }: Props = $props();
  const id = $props.id();
  let pinned = $state(false);
  let dismissed = $state(false);

  function rearm() {
    dismissed = false;
  }

  const dismissible: Attachment<HTMLElement> = (element) => {
    const removers = [
      on(document, 'keydown', (event) => {
        if (event.key === 'Escape' && (pinned || element.matches(':hover, :has(:focus-visible)'))) {
          pinned = false;
          dismissed = true;
        }
      }),
      on(element, 'pointerleave', rearm),
      on(element, 'focusout', rearm),
    ];
    return () => {
      for (const remove of removers) {
        remove();
      }
    };
  };
</script>

<span class="group/tip relative inline-flex" {@attach dismissible}>
  <button
    type="button"
    aria-describedby="{id}-tip"
    onclick={() => {
      pinned = !pinned;
      dismissed = false;
    }}
    onblur={() => (pinned = false)}
    class="inline-flex items-center gap-1.5 underline decoration-white/30 decoration-dotted underline-offset-4 transition hover:text-ink-soft max-sm:-my-3"
  >
    <Icon name="alert" class="size-3.5 shrink-0" stroke={2} />
    {label}
  </button>
  <span
    role="tooltip"
    id="{id}-tip"
    class={[
      'absolute bottom-full left-0 z-30 mb-2 w-max max-w-[min(17rem,calc(100vw-3rem))] origin-bottom-left rounded-pane px-3.5 py-2.5 text-left text-caption text-pretty text-ink-soft glass-pop transition-[opacity,translate,scale] duration-200 before:absolute before:inset-x-0 before:top-full before:h-2',
      dismissed
        ? 'pointer-events-none translate-y-1 scale-[0.96] opacity-0'
        : pinned
          ? 'pointer-events-none translate-y-0 opacity-100'
          : 'pointer-events-none translate-y-1 scale-[0.96] opacity-0 group-hover/tip:pointer-events-auto group-hover/tip:translate-y-0 group-hover/tip:scale-100 group-hover/tip:opacity-100 group-has-focus-visible/tip:translate-y-0 group-has-focus-visible/tip:scale-100 group-has-focus-visible/tip:opacity-100',
    ]}
  >
    <span class="mb-0.5 block font-medium text-ink">{title}</span>{text}
  </span>
</span>
