<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { getI18n } from '../../lib/i18n.svelte.ts';
  import { REPOSITORY_URL } from '../../lib/repository.ts';
  import Icon from '../ui/Icon.svelte';

  const { t } = getI18n();

  type Tab = 'compose' | 'binary' | 'verify';

  const tabs: readonly Tab[] = ['compose', 'binary', 'verify'];
  const indicator = ['translate-x-0', 'translate-x-full', 'translate-x-[200%]'] as const;
  const points = ['host.point.compose', 'host.point.signed', 'host.point.mit'] as const;

  let tab = $state<Tab>('compose');
  let copied = $state(false);
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;

  const index = $derived(tabs.indexOf(tab));
  const terminal = $derived(tabs.map((id) => ({ id, lines: lines(id) })));

  function code(id: Tab): string {
    switch (id) {
      case 'compose':
        return [
          `git clone ${REPOSITORY_URL}`,
          'cd nylzo/deploy/compose',
          'cp .env.example .env',
          'mkdir -p secrets && openssl rand -hex 32 > secrets/pepper',
          'sudo chown 65532:65532 secrets/pepper && sudo chmod 0400 secrets/pepper',
          'docker compose up -d',
        ].join('\n');
      case 'binary':
        return ['openssl rand -hex 32 > pepper && chmod 0400 pepper', 'NYLZO_PEPPER_FILE=pepper nylzo serve'].join('\n');
      case 'verify':
        return `nylzo verify-client --server https://${t('host.domain')}`;
    }
  }

  function lines(id: Tab): { readonly cmd: string; readonly rest: string }[] {
    return code(id)
      .split('\n')
      .map((line) => {
        const space = line.indexOf(' ');
        return space < 0 ? { cmd: line, rest: '' } : { cmd: line.slice(0, space), rest: line.slice(space) };
      });
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(code(tab));
      copied = true;
      clearTimeout(copiedTimer);
      copiedTimer = setTimeout(() => (copied = false), 2000);
    } catch {
      copied = false;
    }
  }

  const cleanup: Attachment = () => () => clearTimeout(copiedTimer);

  function onkeydown(event: KeyboardEvent) {
    const move = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (move !== 0) {
      event.preventDefault();
      const next = tabs[(index + move + tabs.length) % tabs.length] ?? 'compose';
      tab = next;
      document.getElementById(`host-tab-${next}`)?.focus();
    }
  }
</script>

<section id="self-host" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="host-title" {@attach cleanup}>
  <div class="mx-auto grid max-w-[1160px] items-center gap-10 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:gap-16">
    <div class="reveal-left">
      <p class="text-sm font-medium text-aegis-300">{t('host.eyebrow')}</p>
      <h2 id="host-title" class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]">{t('host.title')}</h2>
      <p class="mt-5 max-w-[460px] leading-relaxed text-pretty text-ink-soft">{t('host.sub')}</p>
      <ul class="mt-8 grid gap-3 text-[15px] text-ink-soft">
        {#each points as point (point)}
          <li class="flex items-center gap-3 leading-7">
            <span class="grid size-7 shrink-0 place-items-center rounded-[8px] bg-aegis-500/15 text-aegis-200 ring-1 ring-aegis-400/30"><Icon name="check" class="size-4" /></span>
            {t(point)}
          </li>
        {/each}
      </ul>
    </div>

    <div class="glass reveal-right min-w-0 rounded-card p-2.5 md:p-3">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div class="relative grid grid-cols-3 rounded-control well p-0.5" role="tablist" aria-label={t('host.tabs')}>
          <span
            class={['absolute inset-y-0.5 left-0.5 w-[calc((100%-0.25rem)/3)] rounded-[8px] bg-aegis-500/25 ring-1 ring-aegis-400/50 transition-transform duration-500 ease-silk', indicator[index]]}
            aria-hidden="true"
          ></span>
          {#each tabs as id (id)}
            <button
              id="host-tab-{id}"
              type="button"
              role="tab"
              aria-selected={tab === id}
              aria-controls="host-panel"
              tabindex={tab === id ? 0 : -1}
              onclick={() => (tab = id)}
              {onkeydown}
              class={['relative z-10 h-11 rounded-[8px] px-3 text-[13px] sm:px-4 sm:text-sm md:h-10', tab === id ? 'text-ink' : 'text-ink-soft hover:text-ink']}
            >
              {t(`host.${id}`)}
            </button>
          {/each}
        </div>
        <button type="button" onclick={copy} aria-label={t('host.copy.label')} class="btn-ghost flex h-11 items-center justify-center gap-2 rounded-control px-3.5 text-[13px] md:h-10">
          <span class="stack">
            <span class={['fade-swap flex items-center gap-2', copied ? 'pointer-events-none opacity-0' : 'opacity-100']}>
              <Icon name="copy" class="size-4" />
              {t('host.copy')}
            </span>
            <span class={['fade-swap flex items-center gap-2 text-aegis-200', copied ? 'opacity-100' : 'pointer-events-none opacity-0']}>
              <Icon name="check" stroke={2.2} class="size-4" />
              {t('host.copied')}
            </span>
          </span>
        </button>
        <span class="sr-only" role="status">{copied ? t('host.copied') : ''}</span>
      </div>
      <div class="mt-2.5 overflow-hidden rounded-pane bg-void-950/85 shadow-[0_18px_40px_-18px_oklch(0.05_0.03_275/0.9)] ring-1 ring-white/10">
        <div class="flex items-center gap-2 border-b border-white/8 bg-white/3 px-4 py-2.5" aria-hidden="true">
          <span class="size-2.5 rounded-full bg-[oklch(0.68_0.17_25/0.85)]"></span>
          <span class="size-2.5 rounded-full bg-[oklch(0.82_0.14_85/0.85)]"></span>
          <span class="size-2.5 rounded-full bg-[oklch(0.74_0.16_150/0.85)]"></span>
          <p class="flex-1 text-center font-mono text-[11px] text-ink-mute">{t('host.terminal')}</p>
          <span class="w-[46px]"></span>
        </div>
        <div id="host-panel" role="tabpanel" aria-labelledby="host-tab-{tab}" tabindex="0" class="stack overflow-x-auto p-4 font-mono text-[13px] leading-6 focus-visible:outline-offset-[-2px]">
          {#each terminal as screen (screen.id)}
            <div class={['transition-opacity', tab === screen.id ? 'opacity-100 delay-[160ms] duration-300 ease-out' : 'pointer-events-none opacity-0 duration-200 ease-in']} aria-hidden={tab !== screen.id}>
              {#each screen.lines as line (line.cmd + line.rest)}
                <p class="whitespace-pre"><span class="text-aegis-300 select-none">$ </span><span class="text-aegis-200">{line.cmd}</span><span class="text-ink-soft">{line.rest}</span></p>
              {/each}
              <p class="whitespace-pre" aria-hidden="true">
                <span class="text-aegis-300 select-none">$ </span><span class="inline-block h-[1.05em] w-[0.6em] translate-y-[0.18em] animate-blink bg-aegis-300/80"></span>
              </p>
            </div>
          {/each}
        </div>
      </div>
      <div class="stack mt-3">
        {#each tabs as id (id)}
          <p
            class={['px-1 pb-1 text-sm leading-relaxed text-pretty text-ink-mute transition-opacity', tab === id ? 'opacity-100 delay-[160ms] duration-300 ease-out' : 'pointer-events-none opacity-0 duration-200 ease-in']}
            aria-hidden={tab !== id}
          >
            {t(`host.${id}.note`)}
          </p>
        {/each}
      </div>
    </div>
  </div>
</section>
