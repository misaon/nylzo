<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';
  import { on } from 'svelte/events';

  interface Props {
    readonly id: string;
    readonly label: string;
    readonly open: boolean;
    readonly class?: ClassValue;
    readonly from?: 'above' | 'below';
    readonly onclose: () => void;
    readonly ondismiss: () => void;
    readonly children: Snippet;
  }

  const { id, label, open, class: className, from = 'above', onclose, ondismiss, children }: Props = $props();

  const focusFirst: Attachment<HTMLElement> = (element) => {
    if (open) {
      element
        .querySelector<HTMLElement>(
          'button:not(:disabled, [aria-disabled="true"]), input:not(:disabled), textarea:not(:disabled)',
        )
        ?.focus();
    }
  };

  function onkeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') {
      return;
    }
    event.stopPropagation();
    onclose();
  }

  // The menu holds the trigger too, so focus that leaves from either one closes it.
  const dismissOnLeave: Attachment<HTMLElement> = (element) => {
    const menu = element.closest<HTMLElement>('[data-menu]') ?? element.parentElement ?? element;
    return on(menu, 'focusout', (event) => {
      if (open && event.relatedTarget instanceof Node && !menu.contains(event.relatedTarget)) {
        ondismiss();
      }
    });
  };
</script>

<div
  {id}
  role="dialog"
  tabindex="-1"
  aria-label={label}
  inert={!open}
  {onkeydown}
  {@attach focusFirst}
  {@attach dismissOnLeave}
  class={[
    'absolute z-30 rounded-pane glass-pop',
    open
      ? 'visible translate-y-0 opacity-100 transition-[opacity,translate]'
      : [
          'pointer-events-none invisible opacity-0 transition-[opacity,translate,visibility]',
          from === 'above' ? '-translate-y-1' : 'translate-y-1',
        ],
    className,
  ]}
>
  {@render children()}
</div>
