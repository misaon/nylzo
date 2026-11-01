<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';

  const { translate } = getTranslator();

  const scans = ['keys', 'content', 'passphrase', 'ip', 'ids', 'times'] as const;
  const standards = ['AES-256-GCM', 'HKDF-SHA-256', 'scrypt', 'Ed25519', 'HMAC-SHA-256', 'HPKE'] as const;
</script>

<span id="algoritmy" class="block" aria-hidden="true"></span>

<section id="algorithms" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="extra-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="max-w-[640px] reveal-up">
      <p class="text-sm font-medium text-aegis-300">{translate('extra.eyebrow')}</p>
      <h2
        id="extra-title"
        class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]"
      >
        {translate('extra.title')}
      </h2>
      <p class="mt-5 max-w-[520px] leading-relaxed text-pretty text-ink-soft">{translate('extra.sub')}</p>
    </div>

    <div class="mt-14 grid gap-4 md:gap-5 lg:grid-cols-3">
      <article
        class="glow-on-hover glass flex hover-lift reveal-zoom flex-col rounded-card p-6 md:p-8 lg:col-span-2 lg:row-span-2"
      >
        <h3 class="font-display text-xl font-light tracking-[-0.015em] md:text-2xl">
          {translate('extra.stolen.title')}
        </h3>
        <p class="mt-2 max-w-[520px] leading-relaxed text-pretty text-ink-soft">{translate('extra.stolen.text')}</p>
        <div class="relative mt-8 flex-1 overflow-hidden rounded-pane p-5 well md:p-6">
          <span
            class="pointer-events-none absolute inset-x-0 top-0 h-24 animate-scan bg-linear-to-b from-transparent via-aegis-400/12 to-transparent [animation-duration:4.5s]"
            aria-hidden="true"
          ></span>
          <div class="flex items-center justify-between gap-4">
            <p class="flex items-center gap-2.5 text-sm text-ink">
              <Icon name="search" class="size-4 text-aegis-300" />
              {translate('extra.stolen.copy')}
            </p>
            <span class="rounded-full bg-aegis-500/12 px-3 py-1 text-xs text-aegis-200 ring-1 ring-aegis-400/30"
              >{translate('extra.stolen.found')}</span
            >
          </div>
          <ul class="mt-5 grid gap-2 sm:grid-cols-2">
            {#each scans as scan (scan)}
              <li class="flex items-center gap-3 rounded-[10px] bg-white/3 px-4 py-3 ring-1 ring-white/6">
                <Icon name="check" stroke={2} class="size-4 shrink-0 text-aegis-300" />
                <span class="min-w-0">
                  <span class="block text-sm text-ink">{translate(`extra.scan.${scan}`)}</span>
                  <span class="block text-xs text-ink-mute">{translate(`extra.scan.${scan}.where`)}</span>
                </span>
              </li>
            {/each}
          </ul>
        </div>
      </article>

      <article class="glow-on-hover glass flex hover-lift reveal-up flex-col rounded-card p-6 reveal-step-1 md:p-7">
        <h3 class="text-lg font-medium">{translate('extra.signed.title')}</h3>
        <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">{translate('extra.signed.text')}</p>
        <div class="mt-auto grid gap-2 pt-6 text-sm">
          <p
            class="flex items-center justify-between gap-3 rounded-[10px] bg-white/3 px-3.5 py-2.5 ring-1 ring-white/6"
          >
            <span class="truncate font-mono text-[13px] text-ink-mute">…/r#2.0.Q3tN0xvR8kLp</span>
            <span class="shrink-0 text-xs text-rose-300">{translate('extra.signed.denied')}</span>
          </p>
          <p
            class="flex items-center justify-between gap-3 rounded-[10px] bg-aegis-500/10 px-3.5 py-2.5 ring-1 ring-aegis-400/25"
          >
            <span class="truncate font-mono text-[13px] text-ink-soft"
              >…/r#2.0.Q3tN…<span class="text-aegis-200">.Vh2s</span></span
            >
            <span class="shrink-0 text-xs text-aegis-200">{translate('extra.signed.opens')}</span>
          </p>
        </div>
      </article>

      <article class="glow-on-hover glass flex hover-lift reveal-up flex-col rounded-card p-6 reveal-step-2 md:p-7">
        <h3 class="text-lg font-medium">{translate('extra.padding.title')}</h3>
        <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">{translate('extra.padding.text')}</p>
        <div class="mt-auto grid gap-2.5 pt-6">
          <div class="grid grid-cols-[88px_minmax(0,1fr)] items-center gap-3 text-xs">
            <span class="text-ink-mute">{translate('extra.padding.short')}</span>
            <span class="relative h-3 overflow-hidden rounded-full bg-aegis-500/15 ring-1 ring-aegis-400/25"
              ><span class="absolute inset-y-0 left-0 w-[9%] rounded-full bg-aegis-400"></span></span
            >
          </div>
          <div class="grid grid-cols-[88px_minmax(0,1fr)] items-center gap-3 text-xs">
            <span class="text-ink-mute">{translate('extra.padding.long')}</span>
            <span class="relative h-3 overflow-hidden rounded-full bg-aegis-500/15 ring-1 ring-aegis-400/25"
              ><span class="absolute inset-y-0 left-0 w-[78%] rounded-full bg-aegis-400"></span></span
            >
          </div>
          <p class="flex justify-between text-xs text-ink-mute">
            <span>{translate('extra.padding.server')}</span><span class="font-mono text-aegis-200">4 KiB · 4 KiB</span>
          </p>
        </div>
      </article>

      <article class="glow-on-hover glass flex hover-lift reveal-up flex-col rounded-card p-6 md:p-7">
        <h3 class="text-lg font-medium">{translate('extra.forget.title')}</h3>
        <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">{translate('extra.forget.text')}</p>
        <div class="mt-auto grid gap-2 pt-6 font-mono text-[13px]">
          <p
            class="flex items-center justify-between gap-3 rounded-[10px] bg-white/3 px-3.5 py-2.5 ring-1 ring-white/6"
          >
            <span class="text-ink-mute line-through decoration-rose-300/70">18:40:27</span><span class="text-aegis-200"
              >18:40</span
            >
          </p>
          <p
            class="flex items-center justify-between gap-3 rounded-[10px] bg-white/3 px-3.5 py-2.5 ring-1 ring-white/6"
          >
            <span class="text-ink-mute line-through decoration-rose-300/70">192.0.2.7</span><span
              class="font-sans text-xs text-aegis-200">{translate('extra.forget.ip')}</span
            >
          </p>
        </div>
      </article>

      <article class="glow-on-hover glass flex hover-lift reveal-up flex-col rounded-card p-6 reveal-step-1 md:p-7">
        <h3 class="text-lg font-medium">{translate('extra.twin.title')}</h3>
        <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">{translate('extra.twin.text')}</p>
        <div class="mt-auto flex items-center gap-2 pt-6 text-sm">
          <span class="rounded-[10px] bg-white/4 px-3.5 py-2 font-mono text-[13px] text-ink-soft ring-1 ring-white/10"
            >Go</span
          >
          <span class="h-px flex-1 bg-linear-to-r from-white/15 via-aegis-400/60 to-white/15"></span>
          <span
            class="rounded-full bg-aegis-500/12 px-3 py-1 text-xs whitespace-nowrap text-aegis-200 ring-1 ring-aegis-400/30"
            >{translate('extra.twin.tests')}</span
          >
          <span class="h-px flex-1 bg-linear-to-r from-white/15 via-aegis-400/60 to-white/15"></span>
          <span class="rounded-[10px] bg-white/4 px-3.5 py-2 font-mono text-[13px] text-ink-soft ring-1 ring-white/10"
            >TypeScript</span
          >
        </div>
      </article>

      <article class="glass-soft flex reveal-up flex-col rounded-card p-6 reveal-step-2 md:p-7">
        <h3 class="text-lg font-medium">{translate('extra.standards.title')}</h3>
        <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">{translate('extra.standards.text')}</p>
        <ul class="mt-auto flex flex-wrap gap-1.5 pt-6">
          {#each standards as name (name)}
            <li class="rounded-full bg-white/5 px-2.5 py-1 font-mono text-[11px] text-ink-soft ring-1 ring-white/10">
              {name}
            </li>
          {/each}
        </ul>
      </article>
    </div>
  </div>
</section>
