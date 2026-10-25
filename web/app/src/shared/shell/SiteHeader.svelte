<script lang="ts">
  import { getTranslator } from '../language/translator.svelte.ts';
  import Icon from '../ui/Icon.svelte';
  import Logo from './Logo.svelte';
  import { REPOSITORY_URL } from './repository.ts';

  interface Props {
    readonly variant: 'home' | 'read' | 'page';
  }

  const { variant }: Props = $props();
  const { translate } = getTranslator();

  const sections = [
    { href: '#jak', key: 'nav.how' },
    { href: '#bezpecnost', key: 'nav.security' },
    { href: '#odolnost', key: 'nav.resilience' },
    { href: '#self-host', key: 'nav.selfhost' },
    { href: '#srovnani', key: 'nav.compare' },
  ] as const;
</script>

<header class="sticky top-0 z-40 px-3 pt-3 sm:px-6">
  <nav
    class="glass-soft mx-auto flex h-14 max-w-[1160px] animate-rise items-center justify-between gap-4 rounded-card pr-2 pl-4"
    aria-label={translate('nav.label')}
  >
    {#if variant === 'page'}
      <h1 class="m-0 flex">
        <a href="/" class="flex items-center rounded-control py-1">
          <Logo class="h-7 w-[105px]" label={translate('nav.home')} />
        </a>
      </h1>
    {:else}
      <a
        href={variant === 'home' ? '#napsat' : '/'}
        class="flex items-center rounded-control py-1"
        aria-label={translate(variant === 'home' ? 'nav.top' : 'nav.home')}
      >
        <Logo class="h-7 w-[105px]" />
      </a>
    {/if}

    {#if variant === 'home'}
      <div class="hidden items-center gap-0.5 lg:flex">
        {#each sections as section (section.href)}
          <a
            href={section.href}
            class="rounded-control px-3.5 py-2 text-sm text-ink-soft hover:bg-white/5 hover:text-ink"
            >{translate(section.key)}</a
          >
        {/each}
      </div>
      <a href={REPOSITORY_URL} class="btn-ghost flex h-11 items-center gap-2 rounded-control px-3.5 text-sm md:h-10">
        <svg class="size-[18px]" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
          <path
            d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-1.96c-3.2.7-3.87-1.54-3.87-1.54-.52-1.33-1.28-1.69-1.28-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.69 1.25 3.35.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.42-2.69 5.39-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.68.8.56C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z"
          ></path>
        </svg>
        <span>GitHub</span>
      </a>
    {:else}
      <a href="/" class="btn-ghost flex h-11 items-center gap-2 rounded-control px-3.5 text-sm md:h-10">
        <Icon name="pen" />
        <span class="sm:hidden">{translate('nav.write.short')}</span>
        <span class="hidden sm:inline">{translate('nav.write')}</span>
      </a>
    {/if}
  </nav>
</header>
