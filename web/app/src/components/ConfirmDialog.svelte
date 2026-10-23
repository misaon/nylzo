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

<dialog aria-labelledby={id} {@attach modal} oncancel={() => report(false)} onclose={() => report(false)}>
  <p {id}>{message}</p>
  <button type="button" onclick={() => report(false)}>{dismiss}</button>
  <button type="button" onclick={() => report(true)}>{confirm}</button>
</dialog>
