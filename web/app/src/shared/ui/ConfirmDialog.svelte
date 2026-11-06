<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import Button from './button/Button.svelte';
  import Icon from './icon/Icon.svelte';
  import type { IconName } from './icon/icons.ts';

  interface Props {
    readonly id: string;
    readonly title: string;
    readonly message?: string;
    readonly confirm: string;
    readonly dismiss: string;
    readonly tone?: 'danger' | 'warning';
    readonly icon?: IconName;
    readonly onconfirm: () => void;
    readonly onclose: () => void;
  }

  const { id, title, message, confirm, dismiss, tone = 'danger', icon = 'trash', onconfirm, onclose }: Props = $props();

  let dialog: HTMLDialogElement | undefined;
  let reported = false;

  const modal: Attachment<HTMLDialogElement> = (element) => {
    dialog = element;
    element.showModal();
    return () => {
      dialog = undefined;
    };
  };

  function report(confirmed: boolean) {
    if (reported) {
      return;
    }
    reported = true;
    dialog?.close();
    if (confirmed) {
      onconfirm();
    } else {
      onclose();
    }
  }
</script>

<dialog
  class="confirm w-[min(26rem,calc(100vw-2rem))] max-w-none rounded-card p-6 text-center glass-pop"
  aria-labelledby={id}
  {@attach modal}
  oncancel={() => report(false)}
  onclose={() => report(false)}
>
  <span
    class={[
      'mx-auto grid size-14 place-items-center rounded-full text-white',
      tone === 'danger' ? 'tile-danger' : 'tile-warning',
    ]}
    aria-hidden="true"
  >
    <Icon name={icon} class="size-6" />
  </span>
  <p {id} class="mt-4 text-title font-medium text-pretty text-ink">{title}</p>
  {#if message}
    <p class="mt-1.5 text-body text-pretty text-ink-soft">{message}</p>
  {/if}
  <div class="mt-6 grid grid-cols-2 gap-2">
    <Button variant="ghost" onclick={() => report(false)}>{dismiss}</Button>
    <Button variant={tone === 'danger' ? 'danger' : 'primary'} onclick={() => report(true)}>
      {#if tone === 'danger'}
        <Icon name={icon} class="size-4 shrink-0" animated />
      {/if}
      {confirm}
    </Button>
  </div>
</dialog>
