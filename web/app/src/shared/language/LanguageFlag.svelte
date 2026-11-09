<script lang="ts">
  import { FLAGS } from './flags.ts';
  import type { Language } from './language.ts';

  interface Props {
    readonly language: Language;
  }

  const { language }: Props = $props();
</script>

<span class="inline-flex h-3.5 w-5 shrink-0 overflow-hidden rounded-[3px] ring-1 ring-white/15" aria-hidden="true">
  <svg viewBox="0 0 30 20" class="h-full w-full" preserveAspectRatio="xMidYMid slice" focusable="false">
    {#each FLAGS[language] as shape, index (`${language}-${index}`)}
      <path d={shape.path} fill={shape.fill ?? 'none'} stroke={shape.stroke} stroke-width={shape.strokeWidth}></path>
    {/each}
  </svg>
</span>
