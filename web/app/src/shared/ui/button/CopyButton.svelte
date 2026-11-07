<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { getTranslator } from '../../language/translator.svelte.ts';
  import Icon from '../icon/Icon.svelte';
  import { Flash } from '../transition/flash.svelte.ts';
  import Button from './Button.svelte';

  interface Props {
    readonly text: () => string;
    readonly label?: string | undefined;
    readonly class?: ClassValue;
    readonly labelClass?: ClassValue;
  }

  const COPIED_MS = 2000;

  const { text, label, class: className, labelClass }: Props = $props();
  const { translate } = getTranslator();
  const copied = new Flash(COPIED_MS);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text());
      copied.show();
    } catch {
      copied.hide();
    }
  }
</script>

<Button variant="tint" size="compact" class={className} aria-label={label ?? translate('action.copy')} onclick={copy}>
  <span class="stack justify-items-center">
    <span class={['flex items-center gap-2 fade-swap', copied.shown ? 'pointer-events-none opacity-0' : 'opacity-100']}>
      <Icon name="copy" animated />
      <span class={labelClass}>{translate('action.copy')}</span>
    </span>
    <span
      class={[
        'flex items-center gap-2 text-aegis-200 fade-swap',
        copied.shown ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
    >
      <Icon name="check" stroke={2.2} />
      <span class={labelClass}>{translate('action.copied')}</span>
    </span>
  </span>
</Button>
<span class="sr-only" role="status">{copied.shown ? translate('action.copied') : ''}</span>
