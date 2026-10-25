<script lang="ts">
  import type { IconName } from '../../../shared/ui/icons.ts';
  import type { Composer, Menu } from './composer.svelte.ts';

  import Icon from '../../../shared/ui/Icon.svelte';
  import { CHIP_ACCENT, CHIP_BASE, CHIP_IDLE, CHIP_OPEN, CHIP_PROBLEM } from './chip-classes.ts';

  interface Props {
    readonly composer: Composer;
    readonly menu: Menu;
    readonly popoverId: string;
    readonly icon: IconName;
    readonly label: string;
    readonly ariaLabel?: string | undefined;
    readonly problem: boolean;
    readonly accent?: boolean;
  }

  const { composer, menu, popoverId, icon, label, ariaLabel, problem, accent = false }: Props = $props();

  const open = $derived(composer.menu === menu);
  const tone = $derived.by(() => {
    if (open) {
      return CHIP_OPEN;
    }
    if (problem) {
      return CHIP_PROBLEM;
    }
    return accent ? CHIP_ACCENT : CHIP_IDLE;
  });
</script>

<button
  {@attach composer.focus.attach(menu)}
  type="button"
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-controls={popoverId}
  aria-label={ariaLabel}
  onclick={() => composer.toggleMenu(menu)}
  class={[CHIP_BASE, tone]}
>
  <Icon name={icon} class="size-4 shrink-0" />
  {label}
  <Icon
    name="chevronDown"
    stroke={2}
    class={['size-3.5 shrink-0 opacity-60 transition-transform duration-300', open ? 'rotate-180' : 'rotate-0']}
  />
</button>
