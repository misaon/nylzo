<script lang="ts">
  import { untrack } from 'svelte';

  import SiteFooter from './components/shell/SiteFooter.svelte';
  import SiteHeader from './components/shell/SiteHeader.svelte';
  import Sky from './components/shell/Sky.svelte';
  import { createI18n, setI18n } from './lib/i18n.svelte.ts';
  import { browserPreferences, saveLanguage, type Language } from './lib/language.ts';
  import { HOME_PATHS, screenOf, type Screen } from './lib/pages.ts';
  import Create from './screens/Create.svelte';
  import Read from './screens/Read.svelte';
  import Report from './screens/Report.svelte';
  import Sent from './screens/Sent.svelte';
  import Transparency from './screens/Transparency.svelte';

  interface Props {
    readonly start: { readonly path: string; readonly language: Language };
  }

  const { start }: Props = $props();
  const screen: Screen = $derived(screenOf(start.path));
  const i18n = setI18n(createI18n(untrack(() => start.language)));
  const { t } = i18n;
  const variant = $derived(screen === 'create' ? 'home' : screen === 'read' ? 'read' : 'page');

  function switchLanguage(language: Language) {
    if (i18n.language === language) {
      return;
    }
    i18n.language = language;
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
            <div class="glass legacy-screen rounded-card p-6 md:p-10">
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
            <p role="alert" class="glass rounded-card p-6 text-ink-soft md:p-10">{t('app.crashed')}</p>
          </div>
        {/snippet}
      </svelte:boundary>
    </main>
    <SiteFooter compact={screen === 'read'} current={screen} onlanguage={switchLanguage} />
  </div>
</div>
