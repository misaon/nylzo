<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  interface Props {
    readonly id: string;
    readonly message: string;
    readonly confirm: string;
    readonly dismiss: string;
    readonly onconfirm: () => void;
    readonly onclose: () => void;
  }

  const { id, message, confirm, dismiss, onconfirm, onclose }: Props = $props();

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
  class="confirm"
  aria-labelledby={id}
  {@attach modal}
  oncancel={() => report(false)}
  onclose={() => report(false)}
>
  <p {id} class="m-0 text-[15px] leading-relaxed text-ink-soft">{message}</p>
  <div class="mt-5 grid grid-cols-2 gap-2">
    <button
      type="button"
      class="btn-ghost m-0 h-11 rounded-[9px] px-4 text-[13px] md:h-10"
      onclick={() => report(false)}>{dismiss}</button
    >
    <button
      type="button"
      class="btn-primary m-0 h-11 rounded-[9px] px-4 text-[13px] font-medium md:h-10"
      onclick={() => report(true)}>{confirm}</button
    >
  </div>
</dialog>
