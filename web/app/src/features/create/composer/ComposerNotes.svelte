<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK } from '../../../shared/ui/link/link-classes.ts';
  import ServiceCallout from '../../../shared/ui/notice/ServiceCallout.svelte';
  import { lingering } from '../../../shared/ui/transition/lingering.ts';
  import type { Composer } from './composer.svelte.ts';

  interface Props {
    readonly composer: Composer;
    readonly noticeId: string;
  }

  const { composer, noticeId }: Props = $props();
  const { translate } = getTranslator();

  const failed = $derived(composer.failureNotice !== '');
  const keepProblem = lingering();
  const problemNotice = $derived(keepProblem(composer.problemNotice));
  const quiet = $derived(composer.notice === '' && composer.phase !== 'done');
  const rememberedShown = $derived(composer.remembered === 'remembered' || composer.remembered === 'failed');
  const rememberedText = $derived.by(() => {
    if (composer.remembered === 'remembered') {
      return translate('created.remembered');
    }
    return composer.remembered === 'failed' ? translate('created.not_remembered') : '';
  });
</script>

<div class="relative mt-4 grid text-caption">
  <p
    class={[
      'col-start-1 row-start-1 overflow-y-clip text-center text-balance text-ink-mute transition-[opacity,height] duration-500 [interpolate-size:allow-keywords]',
      quiet ? 'opacity-100' : 'pointer-events-none opacity-0',
      failed && 'h-0',
    ]}
  >
    <Icon
      name="shieldCheck"
      stroke={1.7}
      class="mr-2 inline-block size-4 shrink-0 align-[-3px] text-aegis-300"
    />{translate('compose.note')}
  </p>
  <div
    class={[
      'col-start-1 row-start-1 -my-px overflow-y-clip transition-[opacity,height,padding] duration-500 [interpolate-size:allow-keywords]',
      failed ? 'h-auto py-px opacity-100' : 'pointer-events-none h-0 py-0 opacity-0',
    ]}
    inert={!failed}
  >
    <ServiceCallout id="{noticeId}-failure" message={composer.failureNotice} class="mx-auto max-w-column">
      {#snippet action()}
        {#if composer.loadFailed}
          <Button variant="ghost" size="compact" onclick={() => void composer.loadLimits()}>
            {translate('action.retry')}
          </Button>
        {/if}
      {/snippet}
    </ServiceCallout>
  </div>
  <div
    class={[
      'absolute inset-x-0 top-1/2 flex -translate-y-1/2 items-center justify-center text-center fade-swap',
      composer.problemNotice === '' ? 'pointer-events-none invisible opacity-0' : 'opacity-100',
    ]}
  >
    <p id={noticeId} role="alert" class="font-medium text-balance text-[#fda4af]">{problemNotice}</p>
  </div>
  <div
    class={[
      'absolute inset-x-0 top-1/2 flex -translate-y-1/2 flex-wrap items-center justify-center gap-x-3 gap-y-1 text-center fade-swap',
      rememberedShown ? 'opacity-100' : 'pointer-events-none opacity-0',
    ]}
    inert={!rememberedShown}
  >
    <p role="status" class={['text-balance', composer.remembered === 'failed' ? 'text-rose-300' : 'text-ink-mute']}>
      <Icon
        name={composer.remembered === 'failed' ? 'alert' : 'check'}
        class="mr-2 inline-block size-4 shrink-0 align-[-3px] text-aegis-300"
      />{rememberedText}
    </p>
    <a href="/sent" class={ARROW_LINK}>
      {translate('created.status_link')}
      <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
    </a>
  </div>
</div>
