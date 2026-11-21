<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';
  import { on } from 'svelte/events';
  import { MediaQuery } from 'svelte/reactivity';

  import { BottomSheet } from './bottom-sheet.ts';

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
  const narrow = new MediaQuery('width < 40rem', false);
  const sheet = $derived(narrow.current);
  let bottomSheet: BottomSheet | undefined;

  const focusFirst: Attachment<HTMLElement> = (element) => {
    if (open) {
      element
        .querySelector<HTMLElement>(
          'button:not(:disabled, [aria-disabled="true"]), input:not(:disabled), textarea:not(:disabled)',
        )
        ?.focus({ preventScroll: sheet });
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

  const asBottomSheet: Attachment<HTMLElement> = (element) => {
    const created = new BottomSheet(element, () => ondismiss());
    bottomSheet = created;
    return () => {
      created.destroy();
      bottomSheet = undefined;
    };
  };

  const followOpen: Attachment<HTMLElement> = () => {
    bottomSheet?.setOpen(open);
  };
</script>

{#snippet panel()}
  {#if sheet}
    <span class="mx-auto mb-2 block h-1 w-9 rounded-full bg-white/25" aria-hidden="true"></span>
  {/if}
  {@render children()}
{/snippet}

{#if sheet}
  <div
    {id}
    role="dialog"
    tabindex="-1"
    aria-label={label}
    popover="manual"
    inert={!open}
    {onkeydown}
    {@attach asBottomSheet}
    {@attach followOpen}
    {@attach focusFirst}
    {@attach dismissOnLeave}
    class="bottom-sheet glass-pop"
  >
    {@render panel()}
  </div>
{:else}
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
      'cross-fade absolute z-30 rounded-pane glass-pop',
      from === 'above' ? 'origin-top' : 'origin-bottom',
      open
        ? 'visible translate-y-0 scale-100 opacity-100 transition-[opacity,translate,scale] duration-(--duration-spring-quick) ease-spring'
        : [
            'pointer-events-none invisible scale-[0.96] opacity-0 transition-[opacity,translate,scale,visibility] duration-200',
            from === 'above' ? '-translate-y-1' : 'translate-y-1',
          ],
      className,
    ]}
  >
    {@render panel()}
  </div>
{/if}
