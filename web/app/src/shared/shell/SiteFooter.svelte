<script lang="ts">
  import { BUILD_INFO, buildSourceHref } from '../build-info.ts';
  import type { MessageKey } from '../language/czech.ts';
  import type { Language } from '../language/language.ts';
  import LanguageMenu from '../language/LanguageMenu.svelte';
  import { getTranslator } from '../language/translator.svelte.ts';
  import Logo from './Logo.svelte';
  import { REPOSITORY_URL } from './repository.ts';

  interface Props {
    readonly compact?: boolean;
    readonly current: string;
    readonly onlanguage: (language: Language) => void;
  }

  interface FooterLink {
    readonly href: string;
    readonly key: MessageKey;
    readonly page?: string;
  }

  const { compact = false, current, onlanguage }: Props = $props();
  const { translate } = getTranslator();
  const versionHref = buildSourceHref(BUILD_INFO);
  const LINK = 'transition hover:text-ink';

  const groups: readonly { readonly key: MessageKey; readonly links: readonly FooterLink[] }[] = [
    {
      key: 'footer.product',
      links: [
        { href: '/', key: 'footer.create', page: 'create' },
        { href: '/sent', key: 'footer.sent', page: 'sent' },
        { href: '/transparency', key: 'footer.transparency', page: 'transparency' },
      ],
    },
    {
      key: 'footer.security',
      links: [
        { href: '/#how-it-works', key: 'footer.how' },
        { href: `${REPOSITORY_URL}/blob/main/SECURITY.md`, key: 'footer.vulnerabilities' },
      ],
    },
    {
      key: 'footer.community',
      links: [
        { href: REPOSITORY_URL, key: 'footer.github' },
        { href: '/#self-host', key: 'footer.selfhost' },
        { href: '/agents', key: 'footer.agents', page: 'agents' },
        { href: '/#about', key: 'footer.about' },
      ],
    },
  ];
</script>

{#snippet version(extra: string)}
  {#if versionHref}
    <a
      href={versionHref}
      aria-label={translate('footer.version', { version: BUILD_INFO.version })}
      class={['inline-flex items-center text-micro text-ink-mute tabular-nums', LINK, extra]}>{BUILD_INFO.version}</a
    >
  {:else}
    <span class={['inline-flex items-center text-micro text-ink-mute tabular-nums', extra]}>{BUILD_INFO.version}</span>
  {/if}
{/snippet}

<footer
  class="relative z-10 animate-appear px-3 pb-[max(1.5rem,env(safe-area-inset-bottom))] [animation-delay:240ms] sm:px-6"
>
  {#if compact}
    <div
      class="glass-soft mx-auto flex max-w-page flex-col gap-5 rounded-card p-6 shadow-[inset_0_1px_0_rgba(255,255,255,0.06)] sm:flex-row sm:items-center sm:justify-between md:px-8"
    >
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-5">
        <Logo class="h-7 w-[105px] shrink-0" label="Nylzo" />
        <p class="text-body-sm text-ink-mute">{translate('footer.tagline')}</p>
      </div>
      <div class="flex flex-wrap items-center gap-x-5 gap-y-3 text-body-sm">
        <a href="/#how-it-works" class={['text-ink-soft', LINK]}>{translate('footer.how')}</a>
        <a href={REPOSITORY_URL} class={['text-ink-soft', LINK]}>{translate('footer.github')}</a>
        <div class="flex w-full items-center justify-between gap-4 sm:contents">
          {@render version('')}
          <LanguageMenu placement="footer" {onlanguage} />
        </div>
      </div>
    </div>
  {:else}
    <div class="glass-soft mx-auto max-w-page rounded-card p-6 md:p-10">
      <div class="grid grid-cols-2 gap-x-6 gap-y-8 md:grid-cols-[minmax(0,1.3fr)_repeat(3,minmax(0,1fr))] md:gap-10">
        <div class="col-span-2 md:col-span-1">
          <Logo class="h-8 w-[120px]" label="Nylzo" />
          <p class="mt-4 max-w-[280px] text-body-sm text-ink-mute">{translate('footer.tagline')}</p>
        </div>
        {#each groups as group (group.key)}
          <nav aria-label={translate(group.key)} class="flex flex-col text-ui max-md:gap-0 md:gap-2.5">
            <p class="text-micro text-ink-mute max-md:mb-1">{translate(group.key)}</p>
            {#each group.links as link (link.key)}
              <a
                href={link.href}
                aria-current={link.page !== undefined && link.page === current ? 'page' : undefined}
                class={['w-fit text-ui text-ink-soft max-md:inline-flex max-md:items-center', LINK]}
                >{translate(link.key)}</a
              >
            {/each}
          </nav>
        {/each}
      </div>
      <div
        class="mt-10 flex flex-col gap-3 border-t border-white/8 pt-6 text-caption text-ink-mute sm:flex-row sm:items-center sm:justify-between md:gap-4"
      >
        <div class="flex flex-wrap items-center gap-x-5 gap-y-0 md:gap-y-3">
          <p class="max-md:order-3 max-md:inline-flex max-md:min-h-11 max-md:items-center">
            {translate('footer.note')}
          </p>
          <a
            href="/terms"
            aria-current={current === 'terms' ? 'page' : undefined}
            class={['text-ink-soft max-md:inline-flex max-md:items-center', LINK]}>{translate('footer.terms')}</a
          >
          {@render version('max-md:order-4')}
        </div>
        <LanguageMenu placement="footer" {onlanguage} />
      </div>
    </div>
  {/if}
</footer>
