<script lang="ts">
  import { createI18n, pickLanguage, setI18n } from './lib/i18n.ts';
  import Create from './screens/Create.svelte';
  import Read from './screens/Read.svelte';

  const { t } = setI18n(createI18n(pickLanguage(navigator.languages)));
  const reading = location.pathname.startsWith('/m/');
</script>

<header>
  <h1>{t('app.name')}</h1>
</header>
<main>
  <svelte:boundary>
    {#if reading}
      <Read />
    {:else}
      <Create />
    {/if}

    {#snippet failed()}
      <p role="alert">{t('app.crashed')}</p>
    {/snippet}
  </svelte:boundary>
</main>
