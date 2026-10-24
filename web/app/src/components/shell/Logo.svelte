<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly class?: ClassValue;
    readonly label?: string | undefined;
  }

  const { class: className, label }: Props = $props();
  const uid = $props.id();
  const gradient = `${uid}-mark`;
</script>

<svg class={className} viewBox="36 30 360 96" fill="none" role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : 'true'}>
  <defs>
    <linearGradient id={gradient} x1="0%" y1="0%" x2="100%" y2="100%">
      <stop stop-color="#98D5FF"></stop>
      <stop offset=".52" stop-color="#7AA7F4"></stop>
      <stop offset="1" stop-color="#B4A0F2"></stop>
    </linearGradient>
  </defs>
  <g fill="url(#{gradient})" transform="translate(18 16) scale(1.22)">
    <path d="M16 82V21q0-7 7-7h8l23 30-10 10-14-18v46q0 2-2 2H18q-2 0-2-2Z"></path>
    <path d="M66 14h12q2 0 2 2v61q0 7-7 7h-8L48 61l10-10 8 11Z"></path>
  </g>
  <g stroke="#F1F4FF" transform="translate(153 34) scale(.95)">
    <g fill="none" stroke-width="6.5" stroke-linecap="round" stroke-linejoin="round">
      <path d="M0 72V22m0 20c0-26 38-26 38 0v30"></path>
      <path d="m55 22 19 48m21-48L73 79q-6 17-20 12"></path>
      <path d="M115 1v59q0 12 11 12"></path>
      <path d="M144 22h40l-40 50h40"></path>
      <ellipse cx="226" cy="47" rx="24" ry="26"></ellipse>
    </g>
  </g>
</svg>
