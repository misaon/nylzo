<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import CreatedPanel from '../created/CreatedPanel.svelte';
  import { Composer } from './composer.svelte.ts';
  import ComposerBar from './ComposerBar.svelte';
  import ComposerNotes from './ComposerNotes.svelte';
  import MessageField from './MessageField.svelte';

  const translator = getTranslator();
  const { translate } = translator;
  const composer = new Composer(translator);
  const id = $props.id();
  const noticeId = `${id}-notice`;
</script>

<svelte:window onclick={composer.onWindowClick} onpagehide={composer.leave} onpageshow={composer.returned} />

<section
  id="napsat"
  class="px-4 pt-20 pb-32 sm:px-6 md:pt-28 md:pb-40"
  aria-labelledby="{id}-title"
  {@attach composer.start}
>
  <div class="mx-auto max-w-[860px] text-center">
    <h1
      class="animate-rise-visible font-display text-[36px] leading-[1.12] font-light tracking-[-0.025em] text-balance sm:text-[50px] lg:text-[62px]"
    >
      {translate('hero.title.lead')} <span class="text-aegis-300">{translate('hero.title.accent')}</span>
    </h1>
    <p
      class="mx-auto mt-6 max-w-[640px] animate-rise-visible text-base leading-[1.75] text-balance text-ink-soft [animation-delay:120ms] sm:text-[17px]"
    >
      {translate('hero.intro')}
    </p>
  </div>

  <div class="relative z-20 mx-auto mt-14 max-w-[860px] animate-rise [animation-delay:240ms]">
    <div class="glow-under" aria-hidden="true"></div>
    <div class="glass rounded-card">
      <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
      <h2 id="{id}-title" class="sr-only">{translate('create.title')}</h2>

      <div class="relative stack">
        <form
          class={[
            'flex flex-col text-left fade-swap',
            composer.phase === 'done' ? 'pointer-events-none translate-y-3 opacity-0' : 'translate-y-0 opacity-100',
          ]}
          inert={composer.phase === 'done'}
          onsubmit={composer.submit}
          novalidate
        >
          <MessageField {composer} {noticeId} />
          <ComposerBar {composer} {noticeId} />
        </form>

        {#if composer.sealed}
          <CreatedPanel
            created={composer.sealed.created}
            opensMax={composer.sealed.opensMax}
            passphrase={composer.sealed.passphrase}
            onanother={composer.another}
          />
        {/if}
      </div>
    </div>
  </div>

  <ComposerNotes {composer} {noticeId} />
  <p class="sr-only" role="status" aria-live="polite">{composer.stepTitle}</p>
</section>
