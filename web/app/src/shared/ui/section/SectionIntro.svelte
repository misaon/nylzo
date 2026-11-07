<script lang="ts">
  interface Props {
    readonly id: string;
    readonly eyebrow: string;
    readonly title: string;
  }

  const { id, eyebrow, title }: Props = $props();
</script>

<p class="rv-focus text-eyebrow text-aegis-300 uppercase [--rv-i:0]">{eyebrow}</p>
<h2 {id} class="mt-3 rv-focus font-display text-section font-light text-balance [--rv-i:1] md:text-section-lg">
  {title}
</h2>
