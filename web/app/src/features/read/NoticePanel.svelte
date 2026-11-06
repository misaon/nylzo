<script lang="ts">
  import { formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { isAlarming, NOTICE_ICONS } from './read-notice.ts';
  import type { ReadPage } from './read-page.svelte.ts';
  import { panelClasses } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;

  const title = $derived(translate(page.notice === 'loading' ? 'read.loading' : `open.notice.${page.notice}`));
  const text = $derived.by(() => {
    const { notice, shown } = page;
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
    panelClasses(page.phase === 'notice', page.phase),
  ]}
  inert={page.phase !== 'notice'}
>
  <span
    class={[
      'grid size-20 place-items-center rounded-tile',
      isAlarming(page.notice) ? 'text-white tile-danger' : 'text-ink-soft tile-neutral',
    ]}
    aria-hidden="true"
  >
    <Icon name={NOTICE_ICONS[page.notice]} class="size-9" />
  </span>
  <div>
    <h3 class="font-display text-panel font-light text-balance md:text-panel-lg">{title}</h3>
    <p class="mx-auto mt-2 max-w-[460px] text-body text-pretty text-ink-soft">{text}</p>
  </div>
</div>
