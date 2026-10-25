<script lang="ts">
  import type { Composer } from './composer.svelte.ts';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';

  interface Props {
    readonly composer: Composer;
    readonly noticeId: string;
  }

  const { composer, noticeId }: Props = $props();
  const { translate } = getTranslator();

  const rememberedShown = $derived(composer.remembered === 'remembered' || composer.remembered === 'failed');
  const rememberedText = $derived.by(() => {
    if (composer.remembered === 'remembered') {
      return translate('created.remembered');
    }
    return composer.remembered === 'failed' ? translate('created.not_remembered') : '';
  });
</script>

<div class="relative mt-4 text-xs">
  <p
    class={[
      'flex items-center justify-center gap-2 text-ink-mute fade-swap',
      composer.notice === '' && composer.remembered === 'off' ? 'opacity-100' : 'pointer-events-none opacity-0',
    ]}
  >
    <Icon name="shieldCheck" stroke={1.7} class="size-4 shrink-0 text-aegis-300" />
    {translate('compose.note')}
  </p>
  <div
    class={[
      'absolute inset-x-0 top-1/2 flex -translate-y-1/2 items-center justify-center gap-3 text-center fade-swap',
      composer.notice === '' ? 'pointer-events-none opacity-0' : 'opacity-100',
    ]}
  >
    <p id={noticeId} role="alert" class="text-rose-300">{composer.notice}</p>
    <button
      type="button"
      onclick={composer.loadLimits}
      inert={!composer.loadFailed}
      class={[
        'btn-ghost h-11 rounded-[9px] px-3 text-xs md:h-8',
        composer.loadFailed ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
    >
      {translate('action.retry')}
    </button>
  </div>
  <div
    class={[
      'absolute inset-x-0 top-1/2 flex -translate-y-1/2 flex-wrap items-center justify-center gap-x-3 gap-y-1 text-center fade-swap',
      rememberedShown ? 'opacity-100' : 'pointer-events-none opacity-0',
    ]}
    inert={!rememberedShown}
  >
    <p
      role="status"
      class={['flex items-center gap-2', composer.remembered === 'failed' ? 'text-rose-300' : 'text-ink-mute']}
    >
      <Icon name={composer.remembered === 'failed' ? 'alert' : 'check'} class="size-4 shrink-0 text-aegis-300" />
      {rememberedText}
    </p>
    <a href="/sent" class="inline-flex items-center gap-1.5 text-aegis-300 hover:text-aegis-200">
      {translate('created.status_link')}
      <Icon name="arrowRight" class="size-3.5" />
    </a>
  </div>
</div>
