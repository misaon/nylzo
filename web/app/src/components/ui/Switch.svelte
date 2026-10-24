<script lang="ts">
  interface Props {
    readonly on: boolean;
    readonly size?: 'sm' | 'md';
  }

  const { on, size = 'sm' }: Props = $props();
  const large = $derived(size === 'md');
</script>

<span class={['relative block shrink-0 rounded-full', large ? 'h-7 w-12' : 'h-5 w-9']} aria-hidden="true">
  <span class="absolute inset-0 rounded-full bg-void-950/70 shadow-[inset_0_1px_3px_oklch(0.05_0.03_275/0.8),inset_0_0_0_1px_oklch(1_0_0/0.16)]"></span>
  <span
    class={[
      'absolute inset-0 rounded-full bg-aegis-500/45 shadow-[inset_0_0_0_1px_oklch(0.77_0.115_249/0.55),0_0_18px_oklch(0.56_0.19_258/0.45)] transition-opacity duration-500 ease-silk',
      on ? 'opacity-100' : 'opacity-0',
    ]}
  ></span>
  <span
    class={[
      'absolute rounded-full transition-transform duration-500 ease-silk',
      large ? 'top-1 left-1 size-5' : 'top-0.5 left-0.5 size-4',
      on ? (large ? 'translate-x-5' : 'translate-x-4') : 'translate-x-0',
    ]}
  >
    <span class="absolute inset-0 rounded-full bg-ink-mute/70 shadow-[0_1px_3px_oklch(0.05_0.03_275/0.6)]"></span>
    <span
      class={[
        'absolute inset-0 rounded-full bg-ink shadow-[0_0_0_4px_oklch(0.56_0.19_258/0.25),0_0_14px_oklch(0.77_0.115_249/0.9)] transition-opacity duration-500 ease-silk',
        on ? 'opacity-100' : 'opacity-0',
      ]}
    ></span>
  </span>
</span>
