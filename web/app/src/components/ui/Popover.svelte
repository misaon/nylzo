<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly id: string;
    readonly label: string;
    readonly open: boolean;
    readonly class?: ClassValue;
    readonly from?: 'above' | 'below';
    readonly onclose: () => void;
    readonly children: Snippet;
  }

  const { id, label, open, class: className, from = 'above', onclose, children }: Props = $props();

  const focusFirst: Attachment<HTMLElement> = (element) => {
    if (open) {
      element.querySelector<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled)')?.focus();
    }
  };

  function onkeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onclose();
    }
  }
</script>

<div
  {id}
  role="dialog"
  tabindex="-1"
  aria-label={label}
  inert={!open}
  {onkeydown}
  {@attach focusFirst}
  class={[
    'glass-pop absolute z-30 rounded-pane transition-[opacity,translate] duration-300 ease-silk',
    open ? 'translate-y-0 opacity-100' : ['pointer-events-none opacity-0', from === 'above' ? '-translate-y-1' : 'translate-y-1'],
    className,
  ]}
>
  {@render children()}
</div>
