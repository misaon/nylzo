<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import type { Created } from '../../lib/create.ts';
  import { formatTime } from '../../lib/format.ts';
  import { getI18n } from '../../lib/i18n.svelte.ts';
  import { linkSegments } from '../../lib/link-segments.ts';
  import Chip from '../ui/Chip.svelte';
  import Icon from '../ui/Icon.svelte';

  interface Props {
    readonly created: Created;
    readonly opensMax: number;
    readonly passphrase: boolean;
    readonly onanother: () => void;
  }

  const { created, opensMax, passphrase, onanother }: Props = $props();
  const i18n = getI18n();
  const { t } = i18n;
  const uid = $props.id();

  const BAR_DELAYS = ['delay-[380ms]', 'delay-[490ms]', 'delay-[600ms]', 'delay-[710ms]'] as const;

  let entered = $state(false);
  let copied = $state(false);
  let editing = $state(false);
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;

  const segments = $derived(linkSegments(created.link));

  const enter: Attachment = () => {
    const frame = requestAnimationFrame(() => {
      entered = true;
      document.getElementById(`${uid}-title`)?.focus();
    });
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(copiedTimer);
    };
  };

  async function copy() {
    try {
      await navigator.clipboard.writeText(created.link);
      copied = true;
      clearTimeout(copiedTimer);
      copiedTimer = setTimeout(() => (copied = false), 2400);
    } catch {
      document.getElementById(`${uid}-link`)?.focus();
    }
  }

  function item(delay: string): string[] {
    return entered ? ['translate-y-0 opacity-100 duration-500 ease-silk', delay] : ['translate-y-1.5 opacity-0'];
  }
</script>

<div
  class={['fade-swap absolute inset-0 flex flex-col overflow-hidden text-left', entered ? 'translate-y-0 opacity-100' : 'translate-y-3 opacity-0']}
  {@attach enter}
>
  <div class="flex min-h-0 flex-1 flex-col justify-center gap-5 px-4 py-5 sm:px-6">
    <div class="flex items-center gap-4">
      <span
        class={[
          'grid size-12 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 shadow-[0_0_32px_-6px_oklch(0.56_0.19_258/0.75)] ring-1 ring-aegis-400/35 transition-[opacity,scale]',
          entered ? 'scale-100 opacity-100 delay-[60ms] duration-700 ease-silk' : 'scale-75 opacity-0',
        ]}
        aria-hidden="true"
      >
        <Icon name="check" class="size-6" />
      </span>
      <div class={['min-w-0 transition-[opacity,translate]', item('delay-[120ms]')]}>
        <h2 id="{uid}-title" tabindex="-1" class="font-display text-xl font-light tracking-[-0.015em] outline-none md:text-2xl">{t('created.title')}</h2>
        <p class="mt-0.5 text-sm text-pretty text-ink-mute">{t(passphrase ? 'created.sub.passphrase' : 'created.sub')}</p>
      </div>
    </div>

    <div class={['transition-[opacity,translate]', item('delay-[200ms]')]}>
      <div class={['rounded-pane transition-opacity duration-500', copied ? 'shadow-[0_0_0_1px_oklch(0.77_0.115_249/0.45),0_0_32px_-6px_oklch(0.56_0.19_258/0.8)]' : '']}>
        <div class="well flex items-center gap-3 rounded-pane py-2 pr-2 pl-4">
          <Icon name="lock" class="size-4 shrink-0 text-aegis-300" />
          <div class="relative min-h-11 min-w-0 flex-1 md:min-h-10">
            <p
              class={[
                'pointer-events-none flex h-full min-h-11 min-w-0 items-center font-mono text-[13px] whitespace-nowrap transition-opacity duration-300 md:min-h-10 md:text-[15px]',
                editing ? 'opacity-0' : 'opacity-100',
              ]}
              aria-hidden="true"
            >
              {#each segments as segment, index (segment.id)}
                <span class={['relative px-px pt-1 pb-1.5', segment.tone, segment.server ? 'min-w-0 truncate' : 'shrink-0']}>
                  <span class="sm:hidden">{segment.short}</span>
                  <span class="hidden sm:inline">{segment.text}</span>
                  <span
                    class={[
                      'absolute inset-x-px bottom-0 h-[3px] origin-left rounded-full transition-transform duration-700 ease-silk',
                      segment.bar,
                      entered ? ['scale-x-100', BAR_DELAYS[index]] : 'scale-x-0',
                    ]}
                  ></span>
                </span>
              {/each}
            </p>
            <label for="{uid}-link" class="sr-only">{t('created.link')}</label>
            <input
              id="{uid}-link"
              type="text"
              readonly
              value={created.link}
              onfocus={(event) => {
                editing = true;
                event.currentTarget.select();
              }}
              onblur={() => (editing = false)}
              class={[
                'absolute inset-0 h-full w-full bg-transparent font-mono text-[13px] caret-transparent outline-none md:text-[15px]',
                editing ? 'text-ink-soft' : 'text-transparent',
              ]}
            />
          </div>
          <button
            type="button"
            onclick={copy}
            aria-label={t('created.copy')}
            title={t('created.copy')}
            class="relative grid size-11 shrink-0 place-items-center rounded-[9px] bg-white/4 text-ink-soft ring-1 ring-white/10 hover:bg-white/8 hover:text-ink md:size-9"
          >
            <span
              class={[
                'absolute inset-0 rounded-[9px] bg-aegis-500/20 shadow-[0_0_18px_-4px_oklch(0.56_0.19_258/0.8)] ring-1 ring-aegis-400/50 transition-opacity duration-500 ease-silk',
                copied ? 'opacity-100' : 'opacity-0',
              ]}
            ></span>
            <span class="stack relative place-items-center">
              <Icon name="copy" class={['size-[18px] transition-[opacity,scale] duration-300 ease-silk', copied ? 'scale-50 opacity-0' : 'scale-100 opacity-100']} />
              <Icon name="check" stroke={2.2} class={['size-[18px] text-aegis-200 transition-[opacity,scale] duration-300 ease-silk', copied ? 'scale-100 opacity-100' : 'scale-50 opacity-0']} />
            </span>
          </button>
          <span class="sr-only" role="status">{copied ? t('created.copied') : ''}</span>
        </div>
      </div>
      <div class="mt-2 grid grid-cols-2 gap-2">
        <div class="rounded-pane bg-white/3 px-3.5 py-3 ring-1 ring-white/8">
          <span class="flex gap-1" aria-hidden="true"><span class="h-[3px] w-5 rounded-full bg-ink-mute"></span><span class="h-[3px] w-5 rounded-full bg-nebula-300"></span></span>
          <span class="mt-2.5 block text-[13px] text-ink md:text-sm">{t('created.server.sees')}</span>
          <span class="mt-0.5 block font-mono text-[11px] text-ink-mute">{t('created.server.sees.what')}</span>
        </div>
        <div class="rounded-pane bg-aegis-500/6 px-3.5 py-3 ring-1 ring-aegis-400/20">
          <span class="flex gap-1" aria-hidden="true"><span class="h-[3px] w-5 rounded-full bg-rose-300"></span><span class="h-[3px] w-5 rounded-full bg-aegis-400"></span></span>
          <span class="mt-2.5 block text-[13px] text-ink md:text-sm">{t('created.server.blind')}</span>
          <span class="mt-0.5 block font-mono text-[11px] text-ink-mute">{t('created.server.blind.what')}</span>
        </div>
      </div>
    </div>
  </div>

  <div class={['flex flex-col gap-2 border-t border-white/8 p-2.5 transition-[opacity,translate] sm:flex-row sm:items-center sm:justify-between sm:gap-3', item('delay-[280ms]')]}>
    <ul class="flex flex-wrap items-center gap-1.5">
      <Chip icon="clock">{t('created.chip.expires', { time: formatTime(created.expiresAt, i18n.language) })}</Chip>
      <Chip icon="eye">{t(opensMax === 1 ? 'compose.opens.one' : 'compose.opens.other', { count: opensMax })}</Chip>
      {#if passphrase}
        <Chip icon="key" accent>{t('compose.passphrase.chip')}</Chip>
      {/if}
    </ul>
    <button type="button" onclick={onanother} class="btn-ghost hover-lift flex h-11 w-full items-center justify-center gap-2 rounded-[9px] px-3 text-[13px] whitespace-nowrap sm:w-auto md:h-9">
      <Icon name="plus" class="size-4 shrink-0" />
      {t('created.another')}
    </button>
  </div>
</div>
