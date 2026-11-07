<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import Popover from '../../../shared/ui/menu/Popover.svelte';
  import type { Composer } from './composer.svelte.ts';
  import MenuChip from './MenuChip.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();
  const SEGMENTS = Array.from({ length: 10 }, (_, index) => index + 1);
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
    class="inset-x-0 top-full mt-2 p-1.5 sm:inset-x-auto sm:left-0 sm:w-64"
  >
    <p class="px-3 pt-1.5 pb-1 text-caption text-ink-mute">{translate('compose.opens.title')}</p>
    <div class="mx-1 rounded-pane p-2 well">
      <div class="flex items-center justify-between">
        <button
          type="button"
          disabled={composer.opensMax <= 1}
          onclick={() => (composer.opensMax = Math.max(1, composer.opensMax - 1))}
          aria-label={translate('compose.opens.less')}
          class="group/action button-ghost inline-grid size-11 shrink-0 place-items-center rounded-control transition disabled:opacity-40 md:size-9"
        >
          <Icon name="minus" class="size-4 shrink-0" animated />
        </button>
        <div class="flex flex-col items-center gap-1">
          <output class="text-stat font-light text-ink tabular-nums" aria-live="polite">{composer.opensMax}</output>
          <span class="text-micro text-ink-mute" aria-hidden="true"
            >{translate(composer.opensMax === 1 ? 'compose.opens.unit.one' : 'compose.opens.unit.other')}</span
          >
        </div>
        <button
          type="button"
          disabled={composer.opensMax >= composer.maxOpens}
          onclick={() => (composer.opensMax = Math.min(composer.maxOpens, composer.opensMax + 1))}
          aria-label={translate('compose.opens.more')}
          class="group/action button-ghost inline-grid size-11 shrink-0 place-items-center rounded-control transition disabled:opacity-40 md:size-9"
        >
          <Icon name="plus" class="size-4 shrink-0" animated />
        </button>
      </div>
      <div class="mt-2.5 flex gap-1 px-1 pb-0.5" aria-hidden="true">
        {#each SEGMENTS as segment (segment)}
          <span
            class={[
              'h-1 flex-1 rounded-full transition',
              segment <= composer.opensMax ? 'bg-aegis-300 shadow-glow' : 'bg-white/10',
            ]}
          ></span>
        {/each}
      </div>
    </div>
    {#if composer.opensMax > 1 && !composer.usesPassphrase}
      <p class="px-3 pt-2 pb-1 text-micro text-pretty text-nebula-300">{translate('compose.opens.history')}</p>
    {/if}
  </Popover>
</div>
