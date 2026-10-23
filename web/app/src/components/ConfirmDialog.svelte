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
  const CONFIRMED = 'confirm';

  let dialog: HTMLDialogElement | undefined;

  const modal: Attachment<HTMLDialogElement> = (element) => {
    dialog = element;
    element.showModal();
    return () => {
      dialog = undefined;
    };
  };

  function closed(event: Event & { currentTarget: HTMLDialogElement }) {
    if (event.currentTarget.returnValue === CONFIRMED) {
      onconfirm();
    } else {
      onclose();
    }
  }
</script>

<dialog aria-labelledby={id} {@attach modal} onclose={closed}>
  <p {id}>{message}</p>
  <button type="button" onclick={() => dialog?.close()}>{dismiss}</button>
  <button type="button" onclick={() => dialog?.close(CONFIRMED)}>{confirm}</button>
</dialog>
