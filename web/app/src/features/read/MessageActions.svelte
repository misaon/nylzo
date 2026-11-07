<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { lingering } from '../../shared/ui/transition/lingering.ts';
  import type { ReadPage } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();
  const keepBurnNote = lingering();
  const burnNote = $derived(keepBurnNote(page.burnNote));

  const ACTION_STATES = {
    on: 'opacity-100',
    dim: 'pointer-events-none opacity-40',
    off: 'pointer-events-none opacity-0',
  } as const;

  const state = $derived(page.ready === undefined && page.actions !== 'dim' ? 'off' : page.actions);
  const NOTE_ICON = 'size-4 shrink-0 max-sm:mr-1.5 max-sm:inline-block max-sm:align-[-3px]';
</script>

<div
  class="relative z-30 mx-auto mt-6 flex max-w-column flex-col-reverse items-center gap-6 px-1 sm:flex-row sm:justify-between sm:gap-4"
>
  <div class="stack text-caption">
    <p
      class={[
        'flex items-center justify-center gap-2 text-center text-ink-mute fade-swap max-sm:block max-sm:text-balance',
        page.burnNote ? 'opacity-0' : 'opacity-100',
      ]}
    >
      <Icon name="shieldCheck" stroke={1.7} class={[NOTE_ICON, 'text-aegis-300']} />
      {translate('read.note')}
    </p>
    <p
      class={[
        'flex items-center justify-center gap-2 text-center fade-swap max-sm:block max-sm:text-balance',
        page.burnFailed ? 'text-rose-300' : 'text-ink-soft',
        page.burnNote ? 'opacity-100' : 'invisible opacity-0',
      ]}
    >
      <Icon name={page.burnFailed ? 'alert' : 'check'} stroke={1.7} class={NOTE_ICON} />
      {burnNote}
    </p>
  </div>
  <div
    class={['flex items-center gap-1 transition-opacity duration-500 max-sm:relative', ACTION_STATES[state]]}
    inert={state !== 'on'}
  >
    <Button
      {@attach page.focus.attach('deleteMessage')}
      variant="danger"
      size="compact"
      disabled={page.ready === undefined || page.deleting}
      onclick={() => void page.askDelete()}
      class="transition"
    >
      <Icon name="trash" class="size-4 shrink-0" animated />
      {translate('read.delete')}
    </Button>
  </div>
</div>
