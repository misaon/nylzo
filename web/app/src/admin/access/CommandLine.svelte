<script lang="ts">
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { Flash } from '../../shared/ui/transition/flash.svelte.ts';
  import { getAdminTranslator } from '../messages/translator-context.ts';

  interface Props {
    readonly label: string;
    readonly command: string;
  }

  const COPIED_MS = 2000;

  const { label, command }: Props = $props();
  const { translate } = getAdminTranslator();
  const copied = new Flash(COPIED_MS);

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      copied.show();
    } catch {
      copied.hide();
    }
  }
</script>

<div class="grid gap-1.5">
  <p class="text-micro text-ink-mute">{label}</p>
  <div class="flex items-center gap-2 rounded-pane bg-void-950/85 py-2 pr-2 pl-4 ring-1 ring-white/12">
    <code class="min-w-0 flex-1 font-sans text-caption wrap-anywhere text-ink tabular-nums md:text-body-sm">
      <span class="text-ink-mute/60 select-none" aria-hidden="true">$&nbsp;</span>{command}
    </code>
    <Button
      variant="tint"
      size="compact"
      aria-label={translate('access.copy.label', { command })}
      onclick={() => void copy()}
    >
      <Icon name={copied.shown ? 'check' : 'copy'} />
      <span class="max-sm:sr-only">{translate(copied.shown ? 'access.copied' : 'access.copy')}</span>
    </Button>
  </div>
</div>
<span class="sr-only" role="status">{copied.shown ? translate('access.copied') : ''}</span>
