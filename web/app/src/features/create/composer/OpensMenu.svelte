<script lang="ts">
  import type { Composer } from './composer.svelte.ts';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';
  import Popover from '../../../shared/ui/Popover.svelte';
  import MenuChip from './MenuChip.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();
</script>

<div class="sm:relative" data-menu>
  <MenuChip
    {composer}
    menu="opens"
    popoverId="{id}-opens"
    icon="eye"
    label={composer.opensLabel(composer.opensMax)}
    problem={composer.problemOf.opens !== undefined}
  />
  <Popover
    id="{id}-opens"
    label={translate('compose.opens.title')}
    open={composer.menu === 'opens'}
    onclose={() => composer.closeMenu('opens')}
    class="inset-x-0 top-full mt-2 p-2 sm:inset-x-auto sm:left-0 sm:w-52"
  >
    <p class="px-1 pt-0.5 text-[11px] text-ink-mute">{translate('compose.opens.title')}</p>
    <div class="mt-1.5 flex items-center justify-between rounded-[8px] p-0.5 well">
      <button
        type="button"
        disabled={composer.opensMax <= 1}
        onclick={() => (composer.opensMax = Math.max(1, composer.opensMax - 1))}
        aria-label={translate('compose.opens.less')}
        class="grid size-11 place-items-center rounded-[7px] text-ink-soft hover:bg-white/8 hover:text-ink disabled:opacity-40 md:size-8"
      >
        <Icon name="minus" class="size-3.5" stroke={2} />
      </button>
      <output class="text-[15px] font-medium text-ink tabular-nums" aria-live="polite">{composer.opensMax}</output>
      <button
        type="button"
        disabled={composer.opensMax >= composer.maxOpens}
        onclick={() => (composer.opensMax = Math.min(composer.maxOpens, composer.opensMax + 1))}
        aria-label={translate('compose.opens.more')}
        class="grid size-11 place-items-center rounded-[7px] text-ink-soft hover:bg-white/8 hover:text-ink disabled:opacity-40 md:size-8"
      >
        <Icon name="plus" class="size-3.5" stroke={2} />
      </button>
    </div>
    <p class="mt-1.5 px-1 pb-0.5 text-[11px] leading-relaxed text-ink-mute">
      {translate('compose.opens.note', { max: composer.maxOpens })}
    </p>
  </Popover>
</div>
