<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { VERIFIED_CLIENTS_URL } from '../../shared/shell/repository.ts';
  import CardBars from './CardBars.svelte';
  import MessageActions from './MessageActions.svelte';
  import NoticePanel from './NoticePanel.svelte';
  import OpeningPanel from './OpeningPanel.svelte';
  import { ReadPage } from './read-page.svelte.ts';
  import ReadyPanel from './ReadyPanel.svelte';
  import RevealedPanel from './RevealedPanel.svelte';
  import TrustCards from './TrustCards.svelte';

  const translator = getTranslator();
  const { translate } = translator;
  const page = new ReadPage(translator);
  const id = $props.id();
  const formId = `${id}-form`;
</script>

<svelte:window
  onpagehide={page.leave}
  onbeforeunload={page.forgetPassphrase}
  onpageshow={page.returned}
  onhashchange={page.fragmentArrived}
  onclick={page.onWindowClick}
/>

<section id="open" class="px-4 pt-20 pb-20 sm:px-6 md:pt-28 md:pb-28" aria-labelledby="read-title">
  <p class="sr-only" role="status" aria-live="polite">{page.status}</p>
  <div class="mx-auto max-w-[860px] text-center">
    <p class="animate-rise text-sm font-medium text-aegis-300" aria-hidden="true">{translate('read.title')}</p>
    <h1
      class="mt-3 animate-rise font-display text-[36px] leading-[1.12] font-light tracking-[-0.025em] text-balance [animation-delay:80ms] sm:text-[50px] lg:text-[62px]"
    >
      {translate('open.title')}
    </h1>
    <p
      class="mx-auto mt-6 max-w-[620px] animate-rise text-base leading-[1.75] text-balance text-ink-soft [animation-delay:160ms] sm:text-[17px]"
    >
      {translate('open.intro')}
    </p>
    <a
      href={VERIFIED_CLIENTS_URL}
      rel="noreferrer"
      class="mt-2 inline-flex min-h-11 animate-rise items-center text-[15px] text-aegis-300 underline-offset-4 [animation-delay:200ms] hover:text-aegis-200 hover:underline"
      >{translate('open.verified')}</a
    >
  </div>

  <div class="relative z-20 mx-auto mt-14 max-w-[860px] animate-rise [animation-delay:240ms]">
    <div class="glow-under" aria-hidden="true"></div>
    <div class="glass rounded-card">
      <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
      <h2 id="read-title" {@attach page.focus.attach('readTitle')} tabindex="-1" class="sr-only">
        {translate('read.title')}
      </h2>
      <div class="relative stack min-h-[420px] md:min-h-[440px]">
        <ReadyPanel {page} {formId} />
        <OpeningPanel {page} />
        <RevealedPanel {page} />
        <NoticePanel {page} />
      </div>
      <CardBars {page} {formId} />
    </div>
  </div>

  <MessageActions {page} />
</section>

<TrustCards />
