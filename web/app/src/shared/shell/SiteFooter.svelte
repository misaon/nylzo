<script lang="ts">
  import type { MessageKey } from '../language/czech.ts';
  import type { Language } from '../language/language.ts';

  import { BUILD_INFO, buildSourceHref } from '../build-info.ts';
  import { getTranslator } from '../language/translator.svelte.ts';
  import Logo from './Logo.svelte';
  import { REPOSITORY_URL } from './repository.ts';

  interface Props {
    readonly compact?: boolean;
    readonly current: string;
    readonly onlanguage: (language: Language) => void;
  }

  const { compact = false, current, onlanguage }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const versionHref = buildSourceHref(BUILD_INFO);

  interface FooterLink {
    readonly href: string;
    readonly key: MessageKey;
    readonly page?: string;
  }

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
        { href: '/report', key: 'footer.report', page: 'report' },
        { href: `${REPOSITORY_URL}/blob/main/SECURITY.md`, key: 'footer.vulnerabilities' },
      ],
    },
    {
      key: 'footer.community',
      links: [
        { href: REPOSITORY_URL, key: 'footer.github' },
        { href: '/#self-host', key: 'footer.selfhost' },
        { href: `${REPOSITORY_URL}/tree/main/skills/nylzo`, key: 'footer.skill' },
      ],
    },
  ];

  const languages = [
    { code: 'cs', short: 'CS', label: 'Čeština' },
    { code: 'en', short: 'EN', label: 'English' },
  ] as const;
</script>

{#snippet version()}
  {#if versionHref}
    <a
      href={versionHref}
      aria-label={translate('footer.version', { version: BUILD_INFO.version })}
      class="font-mono text-xs text-ink-mute hover:text-ink">{BUILD_INFO.version}</a
    >
  {:else}
    <span class="font-mono text-xs text-ink-mute">{BUILD_INFO.version}</span>
  {/if}
{/snippet}

{#snippet languageSwitch()}
  <div
    class="relative grid w-fit grid-cols-2 rounded-control p-0.5 well"
    role="group"
    aria-label={translate('language.label')}
  >
    {#each languages as language (language.code)}
      {const active = $derived(translator.language === language.code)}
      <button
        type="button"
        lang={language.code}
        aria-pressed={active}
        onclick={() => onlanguage(language.code)}
        class={[
          'h-11 rounded-[8px] px-4 font-mono text-xs md:h-9',
          active ? 'bg-aegis-500/25 text-aegis-200 ring-1 ring-aegis-400/50' : 'text-ink-soft hover:text-ink',
        ]}
      >
        {language.short}<span class="sr-only"> {language.label}</span>
      </button>
    {/each}
  </div>
{/snippet}

<footer class="relative z-10 px-3 pb-[max(1.5rem,env(safe-area-inset-bottom))] sm:px-6">
  {#if compact}
    <div
      class="glass-soft mx-auto flex max-w-[1160px] flex-col gap-5 rounded-card p-6 sm:flex-row sm:items-center sm:justify-between md:px-8"
    >
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-5">
        <Logo class="h-7 w-[105px] shrink-0" label="Nylzo" />
        <p class="text-sm text-ink-mute">{translate('footer.tagline')}</p>
      </div>
      <div class="flex flex-wrap items-center gap-x-5 gap-y-3 text-sm">
        <a href="/#how-it-works" class="text-ink-soft hover:text-ink">{translate('footer.how')}</a>
        <a href={REPOSITORY_URL} class="text-ink-soft hover:text-ink">{translate('footer.github')}</a>
        {@render version()}
        {@render languageSwitch()}
      </div>
    </div>
  {:else}
    <div class="glass-soft mx-auto max-w-[1160px] rounded-card p-6 md:p-10">
      <div class="grid gap-10 sm:grid-cols-2 md:grid-cols-[minmax(0,1.3fr)_repeat(3,minmax(0,1fr))]">
        <div>
          <Logo class="h-8 w-[120px]" label="Nylzo" />
          <p class="mt-4 max-w-[280px] text-sm leading-relaxed text-ink-mute">{translate('footer.tagline')}</p>
        </div>
        {#each groups as group (group.key)}
          <nav aria-label={translate(group.key)} class="flex flex-col gap-2.5 text-sm">
            <p class="text-xs text-ink-mute">{translate(group.key)}</p>
            {#each group.links as link (link.key)}
              <a
                href={link.href}
                aria-current={link.page !== undefined && link.page === current ? 'page' : undefined}
                class="w-fit text-ink-soft hover:text-ink"
              >
                {translate(link.key)}
              </a>
            {/each}
          </nav>
        {/each}
      </div>
      <div
        class="mt-10 flex flex-col gap-4 border-t border-white/8 pt-6 text-xs text-ink-mute sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="flex flex-wrap items-center gap-x-5 gap-y-3">
          <p>{translate('footer.note')}</p>
          {@render version()}
        </div>
        {@render languageSwitch()}
      </div>
    </div>
  {/if}
</footer>
