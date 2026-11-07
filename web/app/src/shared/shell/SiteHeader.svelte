<script lang="ts">
  import type { Language } from '../language/language.ts';
  import LanguageMenu from '../language/LanguageMenu.svelte';
  import { getTranslator } from '../language/translator.svelte.ts';
  import Button from '../ui/button/Button.svelte';
  import Icon from '../ui/icon/Icon.svelte';
  import GithubMark from './GithubMark.svelte';
  import Logo from './Logo.svelte';
  import { REPOSITORY_URL } from './repository.ts';

  interface Props {
    readonly variant: 'home' | 'read' | 'page' | 'status';
    readonly current?: string;
    readonly onlanguage?: (language: Language) => void;
  }

  const { variant, current, onlanguage }: Props = $props();
  const { translate } = getTranslator();

  const sections = [
    { href: '#how-it-works', key: 'nav.how' },
    { href: '#security', key: 'nav.security' },
    { href: '#resilience', key: 'nav.resilience' },
    { href: '#self-host', key: 'nav.selfhost' },
    { href: '#comparison', key: 'nav.compare' },
  ] as const;
</script>

<header class="sticky top-0 z-40 px-3 pt-3 sm:px-6">
  <nav
    class="glass-soft mx-auto flex h-14 max-w-[1160px] animate-rise items-center justify-between gap-3 rounded-card pr-2 pl-4 sm:gap-4"
    aria-label={translate('nav.label')}
  >
    <a
      href={variant === 'home' ? '#create' : '/'}
      class="flex items-center rounded-control py-1"
      aria-label={translate('nav.home')}
    >
      <Logo class="h-7 w-[105px]" />
    </a>

    {#if variant === 'home'}
      <div class="hidden items-center gap-0.5 lg:flex">
        {#each sections as section (section.href)}
          <a
            href={section.href}
            class="rounded-control px-3.5 py-2 text-ui text-ink-soft transition duration-300 ease-silk hover:bg-white/6 hover:text-ink"
            >{translate(section.key)}</a
          >
        {/each}
      </div>
    {/if}

    <div class="flex shrink-0 items-center gap-1.5">
      {#if variant === 'home' || variant === 'page'}
        <Button
          variant="tint"
          href="/sent"
          id="nav-sent"
          class="max-sm:px-3"
          aria-current={current === 'sent' ? 'page' : undefined}
        >
          <Icon name="shieldCheck" class="size-4 shrink-0 text-aegis-300" animated />
          <span>{translate('nav.sent')}</span>
        </Button>
      {/if}
      {#if variant === 'read' || variant === 'page'}
        <Button variant="ghost" href="/" class="max-[379px]:w-11 max-[379px]:px-0 max-sm:px-3">
          <Icon name="pen" animated />
          <span class="max-[379px]:sr-only sm:hidden">{translate('nav.write.short')}</span>
          <span class="hidden sm:inline">{translate('nav.write')}</span>
        </Button>
      {/if}
      <Button variant="ghost" href={REPOSITORY_URL} class="max-sm:hidden">
        <GithubMark animated />
        <span class="max-sm:sr-only">{translate('footer.github')}</span>
      </Button>
      {#if onlanguage}
        <LanguageMenu placement="header" {onlanguage} class={variant === 'page' ? 'max-sm:hidden' : undefined} />
      {/if}
    </div>
  </nav>
</header>
