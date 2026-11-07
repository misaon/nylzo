<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly text: string;
    readonly layerClass?: ClassValue;
  }

  const { text, layerClass }: Props = $props();
</script>

<p
  class={[
    layerClass,
    'absolute inset-0 [mask-image:radial-gradient(ellipse_70%_68%_at_50%_50%,transparent_42%,black_100%)] px-6 pt-6 text-caption break-all text-ink-soft/15 tabular-nums',
  ]}
>
  {text}
</p>
<div
  class={[
    layerClass,
    'absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-300/6 to-transparent',
  ]}
></div>
<div
  class={[
    layerClass,
    'absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]',
  ]}
></div>
<div
  class={[
    layerClass,
    'absolute inset-0 bg-[radial-gradient(40%_46%_at_50%_40%,oklch(0.67_0.16_252/0.11),oklch(0.72_0.14_305/0.05)_55%,transparent_78%)]',
  ]}
></div>
