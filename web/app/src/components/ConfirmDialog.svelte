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

  const modal: Attachment<HTMLDialogElement> = (dialog) => {
    dialog.showModal();
    return () => dialog.close();
  };
</script>

<dialog aria-labelledby={id} {@attach modal} {onclose}>
  <p {id}>{message}</p>
  <button type="button" onclick={onclose}>{dismiss}</button>
  <button type="button" onclick={onconfirm}>{confirm}</button>
</dialog>
