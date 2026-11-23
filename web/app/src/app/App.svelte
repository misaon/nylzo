<script lang="ts">
  import { tick, untrack } from 'svelte';
  import type { Component } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
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
  import { ScreenWork, setScreenWork } from '../shared/navigation/screen-work.ts';
  import type { UnsavedWork } from '../shared/navigation/screen-work.ts';
  import { sessionSentStore, setSessionSentStore } from '../shared/sent-records/session-sent-store.ts';
  import SiteFooter from '../shared/shell/SiteFooter.svelte';
  import SiteHeader from '../shared/shell/SiteHeader.svelte';
  import Sky from '../shared/shell/Sky.svelte';
  import Button from '../shared/ui/button/Button.svelte';
  import Icon from '../shared/ui/icon/Icon.svelte';
  import ConfirmDialog from '../shared/ui/dialog/ConfirmDialog.svelte';
  import { SCREEN_TITLES, screenOf } from './pages.ts';
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
  const CARD_SCREENS = {
    agents: undefined,
    create: undefined,
    notFound: undefined,
    read: undefined,
    sent: Sent,
    terms: Terms,
    transparency: Transparency,
  } as const satisfies Record<Screen, Component | undefined>;
  // Screens that hold no message secret, so the page can switch between them without loading a new document.
  const SWITCHABLE: ReadonlySet<Screen> = new Set(['agents', 'create', 'sent', 'terms', 'transparency']);
  const LEAVE_CONFIRMED = Symbol('leave confirmed');

  const { start }: Props = $props();
  const { path: startPath, language: startLanguage } = untrack(() => start);
  let path = $state(startPath);
  let visit = $state(0);
  let replacingUrl = false;
  let main: HTMLElement | undefined;
  let leaving = $state<{ readonly unsaved: UnsavedWork; readonly proceed: () => void }>();
  const screen = $derived(screenOf(path));
  const translator = setTranslator(new Translator(startLanguage));
  const tracked = setSessionSentStore(sessionSentStore());
  const work = setScreenWork(new ScreenWork());
  const { translate } = translator;
  const variant = $derived(HEADER_VARIANTS[screen]);
  const CardScreen = $derived(CARD_SCREENS[screen]);
  let requestedLanguage = startLanguage;
  const onlanguage = (language: Language) => void switchLanguage(language);

  const title = $derived(translate(SCREEN_TITLES[screen]));

  $effect(() => {
    document.title = title;
  });

  function replaceAddress(address: string) {
    replacingUrl = true;
    try {
      history.replaceState(history.state, '', address);
    } finally {
      replacingUrl = false;
    }
  }

  function returned() {
    if (location.pathname === path) {
      return;
    }
    // A language switch replaces only the current entry, so an earlier home entry still names the old language.
    if (screen === 'create' && screenOf(location.pathname) === 'create') {
      replaceAddress(`${path}${location.search}${location.hash}`);
      return;
    }
    location.reload();
  }

  // Messages tracked only in this tab live in its memory: moving between screens keeps the document, leaving Nylzo forgets them.
  function keepTrackedMessages(event: NavigateEvent) {
    // A screen still sending a message keeps the old behaviour: a new document cancels the request.
    if (
      replacingUrl ||
      work.busy ||
      !tracked.held() ||
      !event.canIntercept ||
      event.hashChange ||
      event.downloadRequest !== null ||
      event.formData !== null ||
      event.navigationType === 'reload'
    ) {
      return;
    }
    const destination = new URL(event.destination.url);
    const next = screenOf(destination.pathname);
    if (!SWITCHABLE.has(screen) || !SWITCHABLE.has(next)) {
      return;
    }
    if (event.navigationType === 'traverse' && screen === 'create' && next === 'create') {
      event.intercept({
        handler: async () => {
          await tick();
          returned();
        },
      });
      return;
    }
    const { unsaved } = work;
    if (unsaved && event.cancelable && event.info !== LEAVE_CONFIRMED) {
      event.preventDefault();
      leaving = { unsaved, proceed: repeatNavigation(event) };
      return;
    }
    // A home page keeps the language shown, as a reload would through the saved choice.
    path = destination.pathname;
    visit++;
    event.intercept({
      focusReset: 'manual',
      handler: async () => {
        await tick();
        main?.focus({ preventScroll: true });
      },
    });
  }

  const holdMain: Attachment<HTMLElement> = (element) => {
    main = element;
    return () => {
      main = undefined;
    };
  };

  function repeatNavigation(event: NavigateEvent): () => void {
    const { navigationType, destination } = event;
    const options = { info: LEAVE_CONFIRMED };
    if (navigationType === 'traverse') {
      return () => {
        navigation.traverseTo(destination.key, options);
      };
    }
    const history = navigationType === 'replace' ? 'replace' : 'push';
    return () => {
      navigation.navigate(destination.url, { ...options, history });
    };
  }

  function leave() {
    const proceed = leaving?.proceed;
    leaving = undefined;
    proceed?.();
  }

  const listenForNavigation: Attachment = () => {
    if (typeof navigation === 'undefined') {
      return;
    }
    navigation.addEventListener('navigate', keepTrackedMessages);
    return () => navigation.removeEventListener('navigate', keepTrackedMessages);
  };

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
      if (navigator.onLine && screen !== 'create' && screen !== 'read' && !tracked.held()) {
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
      path = homePath(language);
      replaceAddress(`${path}${location.hash}`);
    }
  }
</script>

<svelte:window
  onpopstate={returned}
  onpagehide={() => void tracked.clear()}
  ondragover={keepPageOnFileDrop}
  ondrop={keepPageOnFileDrop}
/>

<div class="relative min-h-dvh overflow-x-clip bg-void-950 font-sans text-ink" {@attach listenForNavigation}>
  <Sky />
  <div class="relative z-10 flex min-h-dvh flex-col">
    <SiteHeader {variant} current={screen} {onlanguage} />
    <main
      id="main"
      tabindex="-1"
      class={['flex-1 outline-none', screen !== 'notFound' && 'min-h-svh']}
      {@attach holdMain}
    >
      {#key visit}
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
            <div
              class={['mx-auto px-4 pt-16 pb-24 sm:px-6 md:pt-24', screen === 'sent' ? 'max-w-wide' : 'max-w-column']}
            >
              <div class="glass animate-rise-visible rounded-card p-6 [animation-delay:80ms] md:p-10">
                <CardScreen />
              </div>
            </div>
          {/if}

          {#snippet failed()}
            <div class="mx-auto max-w-column px-4 pt-16 pb-24 sm:px-6 md:pt-24">
              <div role="alert" class="glass flex flex-col items-start gap-4 rounded-card p-6 md:p-10">
                <p class="text-ink-soft">{translate(screen === 'read' ? 'app.crashed.reopen' : 'app.crashed')}</p>
                {#if screen !== 'read'}
                  <Button variant="ghost" size="compact" href="">
                    <Icon name="refresh" class="size-4 shrink-0" animated />{translate('error.server.reload')}
                  </Button>
                {/if}
              </div>
            </div>
          {/snippet}
        </svelte:boundary>
      {/key}
    </main>
    <SiteFooter compact={screen === 'read'} current={screen} {onlanguage} />
  </div>
</div>

{#if leaving}
  <ConfirmDialog
    id="leave-confirm"
    tone="warning"
    icon="warning"
    title={translate(leaving.unsaved === 'draft' ? 'leave.draft.title' : 'leave.link.title')}
    message={translate(leaving.unsaved === 'draft' ? 'leave.draft.message' : 'leave.link.message')}
    confirmLabel={translate('leave.confirm')}
    dismissLabel={translate('leave.stay')}
    onconfirm={leave}
    onclose={() => (leaving = undefined)}
  />
{/if}
