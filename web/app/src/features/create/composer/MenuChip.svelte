<script lang="ts">
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../shared/ui/icon/icons.ts';
  import { CHIP_ACCENT, CHIP_IDLE, CHIP_LAYOUT, CHIP_OPEN, CHIP_PROBLEM } from './chip-classes.ts';
  import type { Composer, Menu } from './composer.svelte.ts';

  interface Props {
    readonly composer: Composer;
    readonly menu: Menu;
    readonly popoverId: string;
    readonly icon: IconName;
    readonly label: string;
    readonly reserve?: string;
    readonly problem: boolean;
    readonly accent?: boolean;
  }

  const { composer, menu, popoverId, icon, label, reserve, problem, accent = false }: Props = $props();
  const open = $derived(composer.menu === menu);
</script>

<button
  {@attach composer.focus.attach(menu)}
  type="button"
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-controls={popoverId}
  onclick={() => composer.toggleMenu(menu)}
  class={[
    'group/action',
    CHIP_LAYOUT,
    problem && !open ? CHIP_PROBLEM : accent ? CHIP_ACCENT : CHIP_IDLE,
    open && CHIP_OPEN,
  ]}
>
  <Icon name={icon} class="size-4 shrink-0" animated />
  <span class="grid">
    <span class="col-start-1 row-start-1">{label}</span>
    {#if reserve !== undefined}
      <span class="invisible col-start-1 row-start-1" aria-hidden="true">{reserve}</span>
    {/if}
  </span>
  <Icon
    name="chevronDown"
    stroke={2}
    class={['size-3.5 shrink-0 opacity-70 transition-transform', open ? 'rotate-180' : 'rotate-0']}
  />
</button>
