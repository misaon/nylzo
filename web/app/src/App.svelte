<script lang="ts">
  import { browserPreferences, createI18n, otherLanguage, saveLanguage, savedLanguage, setI18n } from './lib/i18n.svelte.ts';
  import Create from './screens/Create.svelte';
  import Read from './screens/Read.svelte';
  import Report from './screens/Report.svelte';
  import Sent from './screens/Sent.svelte';
  import Transparency from './screens/Transparency.svelte';

  const preferences = browserPreferences();
  const i18n = setI18n(createI18n(savedLanguage(preferences, navigator.languages)));
  const { t } = i18n;
  const path = location.pathname;
  const screen =
    path === '/sent' ? 'sent' : path === '/report' ? 'report' : path === '/transparency' ? 'transparency' : path.startsWith('/m/') ? 'read' : 'create';
  const other = $derived(otherLanguage(i18n.language));

  function switchLanguage() {
    i18n.language = other;
    document.documentElement.lang = i18n.language;
    saveLanguage(preferences, i18n.language);
  }
</script>

<header>
  <h1>{t('app.name')}</h1>
  <nav aria-label={t('nav.label')}>
    <a href="/" aria-current={screen === 'create' ? 'page' : undefined}>{t('nav.create')}</a>
    <a href="/sent" aria-current={screen === 'sent' ? 'page' : undefined}>{t('nav.sent')}</a>
    <a href="/report" aria-current={screen === 'report' ? 'page' : undefined}>{t('nav.report')}</a>
    <a href="/transparency" aria-current={screen === 'transparency' ? 'page' : undefined}>{t('nav.transparency')}</a>
  </nav>
  <button type="button" lang={other} onclick={switchLanguage}>{t('language.other')}</button>
</header>
<main>
  <svelte:boundary>
    {#if screen === 'read'}
      <Read />
    {:else if screen === 'sent'}
      <Sent />
    {:else if screen === 'report'}
      <Report />
    {:else if screen === 'transparency'}
      <Transparency />
    {:else}
      <Create />
    {/if}

    {#snippet failed()}
      <p role="alert">{t('app.crashed')}</p>
    {/snippet}
  </svelte:boundary>
</main>
