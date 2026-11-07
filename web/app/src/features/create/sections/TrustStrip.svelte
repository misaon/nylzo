<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../shared/ui/icon/icons.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import { TRUST_LOGOS } from './trust-logos.ts';

  const { translate } = getTranslator();

  const glyphs: readonly { readonly icon: IconName; readonly name: string }[] = [
    { icon: 'lock', name: 'AES-256-GCM' },
    { icon: 'key', name: 'HKDF-SHA-256' },
  ];
  const license: { readonly icon: IconName; readonly name: string } = $derived({
    icon: 'document',
    name: translate('trust.license'),
  });
</script>

{#snippet glyph(item: { readonly icon: IconName; readonly name: string })}
  <li class="flex items-center gap-3 text-title whitespace-nowrap text-ink-soft">
    <Icon name={item.icon} class="size-8 shrink-0" stroke={1.7} paint="url(#trust-tone)" />
    {item.name}
  </li>
{/snippet}

<section
  id="trust-strip"
  aria-label={translate('trust.label')}
  class="relative px-4 pt-20 pb-24 sm:px-6 md:pt-28 md:pb-32"
  {@attach reveal}
>
  <svg class="absolute size-0" aria-hidden="true" focusable="false">
    <defs>
      <linearGradient id="trust-tone" x1="0" y1="0" x2="1" y2="1">
        <stop stop-color="#98D5FF"></stop>
        <stop offset=".52" stop-color="#7AA7F4"></stop>
        <stop offset="1" stop-color="#B4A0F2"></stop>
      </linearGradient>
    </defs>
  </svg>
  <p class="reveal-focus text-center text-caption text-balance text-ink-mute">{translate('trust.title')}</p>
  <div
    class="relative mx-auto mt-8 max-w-page reveal-part overflow-hidden [mask-image:linear-gradient(90deg,transparent,black_12%,black_88%,transparent)] [--reveal-delay:200ms] md:mt-10"
  >
    <div class="flex w-max motion-safe:animate-trust-marquee">
      {#each [false, true] as copy (copy)}
        <ul
          role="list"
          class="flex items-center gap-14 px-7 md:gap-20 md:px-10"
          aria-hidden={copy ? 'true' : undefined}
        >
          {#each glyphs as item (item.name)}
            {@render glyph(item)}
          {/each}
          {#each TRUST_LOGOS as logo (logo.name)}
            <li class="flex items-center gap-3 text-title whitespace-nowrap text-ink-soft">
              <svg
                class={[logo.size, 'shrink-0']}
                viewBox="0 0 24 24"
                fill="url(#trust-tone)"
                role={logo.labelOnly ? 'img' : undefined}
                aria-label={logo.labelOnly ? logo.name : undefined}
                aria-hidden={logo.labelOnly ? undefined : 'true'}
                focusable="false"
              >
                <path d={logo.path}></path>
              </svg>
              {#if !logo.labelOnly}{logo.name}{/if}
            </li>
          {/each}
          {@render glyph(license)}
        </ul>
      {/each}
    </div>
  </div>
</section>
