<script lang="ts">
  import { untrack } from 'svelte';

  import Create from '../features/create/Create.svelte';
  import Read from '../features/read/Read.svelte';
  import Report from '../features/report/Report.svelte';
  import Sent from '../features/sent/Sent.svelte';
  import Transparency from '../features/transparency/Transparency.svelte';
  import { browserPreferences, type Language, saveLanguage } from '../shared/language/language.ts';
  import { setTranslator, Translator } from '../shared/language/translator.svelte.ts';
  import SiteFooter from '../shared/shell/SiteFooter.svelte';
  import SiteHeader from '../shared/shell/SiteHeader.svelte';
  import Sky from '../shared/shell/Sky.svelte';
  import { HOME_PATHS, type Screen, screenOf } from './pages.ts';

  interface Props {
    readonly start: { readonly path: string; readonly language: Language };
  }

  const HEADER_VARIANTS = {
    create: 'home',
    read: 'read',
    report: 'page',
    sent: 'page',
    transparency: 'page',
  } as const satisfies Record<Screen, 'home' | 'page' | 'read'>;

  const { start }: Props = $props();
  const screen: Screen = $derived(screenOf(start.path));
  const translator = setTranslator(new Translator(untrack(() => start.language)));
  const { translate } = translator;
  const variant = $derived(HEADER_VARIANTS[screen]);

  function switchLanguage(language: Language) {
    if (translator.language === language) {
      return;
    }
    translator.language = language;
    document.documentElement.lang = language;
    saveLanguage(browserPreferences(), language);
    if (screen === 'create') {
      history.replaceState(history.state, '', `${HOME_PATHS[language]}${location.hash}`);
    }
  }
</script>

<div class="relative min-h-dvh overflow-x-clip bg-void-950 font-sans text-ink">
  <Sky />
  <div class="relative z-10 flex min-h-dvh flex-col">
    <SiteHeader {variant} />
    <main class="min-h-svh flex-1">
      <svelte:boundary>
        {#if screen === 'read'}
          <Read />
        {:else if screen === 'create'}
          <Create />
        {:else}
          <div class="mx-auto max-w-[860px] px-4 pt-16 pb-24 sm:px-6 md:pt-24">
            <div class="glass rounded-card p-6 md:p-10">
              {#if screen === 'sent'}
                <Sent />
              {:else if screen === 'report'}
                <Report />
              {:else}
                <Transparency />
              {/if}
            </div>
          </div>
        {/if}

        {#snippet failed()}
          <div class="mx-auto max-w-[860px] px-4 pt-16 pb-24 sm:px-6 md:pt-24">
            <p role="alert" class="glass rounded-card p-6 text-ink-soft md:p-10">{translate('app.crashed')}</p>
          </div>
        {/snippet}
      </svelte:boundary>
    </main>
    <SiteFooter compact={screen === 'read'} current={screen} onlanguage={switchLanguage} />
  </div>
</div>
