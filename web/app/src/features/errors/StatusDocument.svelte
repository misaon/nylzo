<script lang="ts">
  import { untrack } from 'svelte';

  import type { Language } from '../../shared/language/language.ts';
  import { setTranslator, Translator } from '../../shared/language/translator.svelte.ts';
  import SiteHeader from '../../shared/shell/SiteHeader.svelte';
  import Sky from '../../shared/shell/Sky.svelte';
  import Maintenance from './Maintenance.svelte';
  import ServerError from './ServerError.svelte';
  import { MAINTENANCE_RELOAD_SECONDS } from './status-page.ts';
  import type { StatusPage } from './status-page.ts';

  interface Props {
    readonly page: StatusPage;
    readonly language: Language;
  }

  const { page, language }: Props = $props();
  const { translate } = setTranslator(new Translator(untrack(() => language)));
</script>

<svelte:head>
  <title>{translate(page === 'error' ? 'title.serverError' : 'title.maintenance')}</title>
  {#if page === 'maintenance'}
    <meta http-equiv="refresh" content={String(MAINTENANCE_RELOAD_SECONDS)} />
  {/if}
</svelte:head>

<div class="relative overflow-x-clip bg-void-950 font-sans text-ink">
  <Sky />
  <div class="relative z-10 flex min-h-dvh flex-col">
    <SiteHeader variant="status" />
    <main class="grid flex-1 place-items-center">
      {#if page === 'error'}
        <ServerError />
      {:else}
        <Maintenance />
      {/if}
    </main>
  </div>
</div>
