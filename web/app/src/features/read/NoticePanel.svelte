<script lang="ts">
  import type { ReadPage } from './read-page.svelte.ts';

  import { formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import OpenLinkForm from '../../shared/link/OpenLinkForm.svelte';
  import Icon from '../../shared/ui/Icon.svelte';
  import { isAlarming, type Notice, NOTICE_ICONS } from './read-notice.ts';
  import { PANEL_HIDDEN, PANEL_SHOWN } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const PASTE_NOTICES: ReadonlySet<Notice> = new Set(['invalid', 'removed', 'reopen']);

  const { page }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;

  const title = $derived(translate(page.notice === 'loading' ? 'read.loading' : `open.notice.${page.notice}`));
  const text = $derived.by(() => {
    const { notice, shown } = page;
    if (notice === 'unknown' || notice === 'cleared') {
      return translate(`read.${notice}`);
    }
    if (notice === 'busy') {
      const retryAfter = shown.view === 'retry_later' ? shown.retryAfter : undefined;
      return translate('open.notice.busy.text', { time: formatWait(retryAfter, translator.language) });
    }
    return translate(notice === 'loading' ? 'open.loading.text' : `open.notice.${notice}.text`);
  });
</script>

<div
  class={[
    'flex flex-col items-center justify-center gap-7 px-5 py-12 text-center fade-swap sm:px-10 md:py-14',
    page.phase === 'notice' ? PANEL_SHOWN : PANEL_HIDDEN,
  ]}
  inert={page.phase !== 'notice'}
>
  <span
    class={[
      'grid size-20 place-items-center rounded-[22px] ring-1',
      isAlarming(page.notice)
        ? 'bg-rose-500/12 text-rose-300 ring-rose-300/35'
        : 'bg-white/4 text-ink-soft ring-white/12',
    ]}
    aria-hidden="true"
  >
    <Icon name={NOTICE_ICONS[page.notice]} stroke={1.6} class="size-9" />
  </span>
  <div>
    <h3 class="font-display text-2xl font-light tracking-[-0.02em] md:text-[28px]">{title}</h3>
    <p class="mx-auto mt-2 max-w-[460px] text-[15px] leading-relaxed text-pretty text-ink-mute">{text}</p>
  </div>
  {#if PASTE_NOTICES.has(page.notice)}
    <div class="w-full max-w-[460px]">
      <OpenLinkForm />
    </div>
  {/if}
</div>
