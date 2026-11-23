<script lang="ts">
  import { untrack } from 'svelte';

  import Logo from '../shared/shell/Logo.svelte';
  import Sky from '../shared/shell/Sky.svelte';
  import Button from '../shared/ui/button/Button.svelte';
  import Icon from '../shared/ui/icon/Icon.svelte';
  import Access from './access/Access.svelte';
  import type { Range } from './api/types.ts';
  import Dashboard from './dashboard/Dashboard.svelte';
  import { adminTranslator } from './messages/catalog.ts';
  import type { AdminLanguage } from './messages/catalog.ts';
  import { setAdminTranslator } from './messages/translator-context.ts';
  import { AdminArea } from './session/admin-area.svelte.ts';
  import { createAdminClient } from './session/client.ts';

  interface Props {
    readonly language: AdminLanguage;
  }

  const { language }: Props = $props();
  const { translate } = setAdminTranslator(adminTranslator(untrack(() => language)));
  const time = new Intl.DateTimeFormat(
    untrack(() => language),
    { timeStyle: 'short' },
  );
  const area = new AdminArea({ client: createAdminClient(), now: Date.now, hidden: () => document.hidden });
  const expires = $derived(area.expiresAt === undefined ? undefined : time.format(area.expiresAt * 1000));
  void area.start();
</script>

<svelte:document onvisibilitychange={area.wake} />

<div class="relative min-h-dvh overflow-x-clip bg-void-950 font-sans text-ink">
  <Sky />
  <div class="relative z-10 flex min-h-dvh flex-col">
    <header class="sticky top-0 z-40 px-3 pt-3 sm:px-6">
      <div
        class="glass-soft mx-auto flex h-14 max-w-page animate-rise items-center justify-between gap-3 rounded-card pr-2 pl-4"
      >
        <div class="flex min-w-0 items-center gap-3">
          <Logo class="h-7 w-[105px] shrink-0" label="Nylzo" />
          <span class="h-5 w-px bg-white/12" aria-hidden="true"></span>
          <span class="truncate text-ui text-ink-soft">{translate('access.area')}</span>
        </div>
        {#if area.stage === 'signedIn'}
          <div class="flex shrink-0 items-center gap-3">
            {#if expires}
              <p class="text-caption text-ink-mute tabular-nums max-sm:hidden">
                {translate('access.expires', { time: expires })}
              </p>
            {/if}
            <Button variant="ghost" onclick={() => void area.signOut()}>
              <Icon name="userOff" />{translate('access.signOut')}
            </Button>
          </div>
        {/if}
      </div>
    </header>
    <main class="flex-1 px-4 pt-12 pb-16 sm:px-6 md:pt-16">
      {#if area.stage === 'signedIn'}
        <div class="mx-auto max-w-page">
          <Dashboard
            overview={area.overview}
            live={area.live}
            range={area.range}
            failed={area.failed}
            stale={area.stale}
            onrange={(range: Range) => area.chooseRange(range)}
          />
        </div>
      {:else}
        <Access {area} />
      {/if}
    </main>
    <footer class="flex justify-center px-4 pb-8">
      <Button variant="ghost" href="/">
        <Icon name="arrowRight" class="size-4 rotate-180" />{translate('access.back')}
      </Button>
    </footer>
  </div>
</div>
