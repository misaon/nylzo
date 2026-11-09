<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { prefersReducedMotion } from 'svelte/motion';

  import Agents from '../features/agents/Agents.svelte';
  import Create from '../features/create/Create.svelte';
  import NotFound from '../features/errors/NotFound.svelte';
  import Read from '../features/read/Read.svelte';
  import Sent from '../features/sent/Sent.svelte';
  import Terms from '../features/terms/Terms.svelte';
  import Transparency from '../features/transparency/Transparency.svelte';
  import { loadCatalog } from '../shared/language/catalog.ts';
  import { browserPreferences, homePath, saveLanguage } from '../shared/language/language.ts';
  import type { Language } from '../shared/language/language.ts';
  import { setTranslator, Translator } from '../shared/language/translator.svelte.ts';
  import SiteFooter from '../shared/shell/SiteFooter.svelte';
  import SiteHeader from '../shared/shell/SiteHeader.svelte';
  import Sky from '../shared/shell/Sky.svelte';
  import { screenOf } from './pages.ts';
  import type { Screen } from './pages.ts';

  interface Props {
    readonly start: { readonly path: string; readonly language: Language };
  }

  const HEADER_VARIANTS = {
    agents: 'page',
    create: 'home',
    notFound: 'page',
    read: 'read',
    sent: 'page',
    terms: 'page',
    transparency: 'page',
  } as const satisfies Record<Screen, 'home' | 'page' | 'read'>;

  const { start }: Props = $props();
  const { path, language: startLanguage } = untrack(() => start);
  const screen: Screen = screenOf(path);
  const translator = setTranslator(new Translator(startLanguage));
  const { translate } = translator;
  const variant = HEADER_VARIANTS[screen];
  let requestedLanguage = startLanguage;
  const onlanguage = (language: Language) => void switchLanguage(language);

  function returned() {
    if (location.pathname !== path) {
      location.reload();
    }
  }

  // A file dropped outside a drop zone would replace the page, and with it a message shown once; a file input keeps its drop.
  function keepPageOnFileDrop(event: DragEvent) {
    const fileInput = event.target instanceof HTMLInputElement && event.target.type === 'file';
    if (!fileInput && event.dataTransfer?.types.includes('Files')) {
      event.preventDefault();
    }
  }

  async function switchLanguage(language: Language) {
    requestedLanguage = language;
    if (translator.language === language) {
      return;
    }
    try {
      await loadCatalog(language);
    } catch {
      // A release removed this build's catalogs; a page that holds no message loads again from the new build.
      saveLanguage(browserPreferences(), language);
      if (navigator.onLine && screen !== 'create' && screen !== 'read') {
        location.reload();
      }
      return;
    }
    if (requestedLanguage !== language) {
      return;
    }
    const applyLanguage = async () => {
      translator.language = language;
      document.documentElement.lang = language;
      await tick();
    };
    if (prefersReducedMotion.current || typeof document.startViewTransition !== 'function') {
      void applyLanguage();
    } else {
      document.startViewTransition(applyLanguage);
    }
    saveLanguage(browserPreferences(), language);
    if (screen === 'create') {
      history.replaceState(history.state, '', `${homePath(language)}${location.hash}`);
    }
  }
</script>

<svelte:window onpopstate={returned} ondragover={keepPageOnFileDrop} ondrop={keepPageOnFileDrop} />

<div class="relative min-h-dvh overflow-x-clip bg-void-950 font-sans text-ink">
  <Sky />
  <div class="relative z-10 flex min-h-dvh flex-col">
    <SiteHeader {variant} current={screen} {onlanguage} />
    <main class={['flex-1', screen !== 'notFound' && 'min-h-svh']}>
      <svelte:boundary>
        {#if screen === 'read'}
          <Read />
        {:else if screen === 'create'}
          <Create />
        {:else if screen === 'agents'}
          <Agents />
        {:else if screen === 'notFound'}
          <NotFound />
        {:else}
          <div class="mx-auto max-w-column px-4 pt-16 pb-24 sm:px-6 md:pt-24">
            <div class="glass animate-rise rounded-card p-6 [animation-delay:80ms] md:p-10">
              {#if screen === 'sent'}
                <Sent />
              {:else if screen === 'terms'}
                <Terms />
              {:else}
                <Transparency />
              {/if}
            </div>
          </div>
        {/if}

        {#snippet failed()}
          <div class="mx-auto max-w-column px-4 pt-16 pb-24 sm:px-6 md:pt-24">
            <p role="alert" class="glass rounded-card p-6 text-ink-soft md:p-10">{translate('app.crashed')}</p>
          </div>
        {/snippet}
      </svelte:boundary>
    </main>
    <SiteFooter compact={screen === 'read'} current={screen} {onlanguage} />
  </div>
</div>
