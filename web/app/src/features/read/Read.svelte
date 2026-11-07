<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import CardBars from './CardBars.svelte';
  import MessageActions from './MessageActions.svelte';
  import NoticePanel from './NoticePanel.svelte';
  import OpeningPanel from './OpeningPanel.svelte';
  import { isEnded } from './read-notice.ts';
  import { ReadPage } from './read-page.svelte.ts';
  import ReadyPanel from './ReadyPanel.svelte';
  import RevealedPanel from './revealed/RevealedPanel.svelte';
  import TrustCards from './TrustCards.svelte';

  const translator = getTranslator();
  const { translate } = translator;
  const page = new ReadPage(translator);
  const id = $props.id();
  const formId = `${id}-form`;
  const ended = $derived(page.phase === 'notice' && isEnded(page.notice));
</script>

<svelte:head>
  <title>{translate('title.read')}</title>
</svelte:head>

<svelte:window
  onpagehide={page.leave}
  onbeforeunload={page.forgetPassphrase}
  onpageshow={page.returned}
  onhashchange={page.fragmentArrived}
/>

<section id="open" class="px-4 pt-16 pb-20 sm:px-6 md:pt-24 md:pb-28" aria-labelledby="read-title">
  <p class="sr-only" role="status" aria-live="polite">{page.status}</p>
  <div class="mx-auto flex max-w-column flex-col items-center text-center">
    <p class="animate-rise text-eyebrow text-aegis-300 uppercase" aria-hidden="true">{translate('read.title')}</p>
    <h1 class="mt-3 animate-rise font-display text-hero font-light text-balance [animation-delay:80ms] md:text-hero-lg">
      {translate(ended ? 'open.title.ended' : 'open.title')}
    </h1>
    <div
      class={[
        'transition-[opacity,height,padding] duration-500 [interpolate-size:allow-keywords]',
        ended ? 'h-0 overflow-y-clip pt-0 opacity-0' : 'h-auto pt-5 opacity-100',
      ]}
      inert={ended}
    >
      <p
        class="mx-auto max-w-[560px] animate-rise text-lead text-pretty text-ink-soft [animation-delay:160ms] md:text-lead-lg"
      >
        {translate('open.intro')}
      </p>
    </div>
  </div>

  <div class="relative z-20 mx-auto mt-10 max-w-column animate-rise [animation-delay:240ms]">
    <div class="glow-under" aria-hidden="true"></div>
    <div class="glass rounded-card shadow-[0_30px_80px_-30px_rgb(40_70_200/0.45),inset_0_1px_0_rgb(255_255_255/0.08)]">
      <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
      <h2 id="read-title" {@attach page.focus.attach('readTitle')} tabindex="-1" class="sr-only">
        {translate('read.title')}
      </h2>
      <div class={['relative stack', page.phase !== 'revealed' && 'min-h-[420px] md:min-h-[440px]']}>
        <ReadyPanel {page} {formId} />
        <OpeningPanel {page} />
        <RevealedPanel {page} />
        <NoticePanel {page} />
      </div>
      <CardBars {page} />
    </div>
  </div>

  <MessageActions {page} />
</section>

<TrustCards />
