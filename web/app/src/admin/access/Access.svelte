<script lang="ts">
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../shared/ui/icon/icons.ts';
  import ServiceCallout from '../../shared/ui/notice/ServiceCallout.svelte';
  import StateCard from '../../shared/ui/notice/StateCard.svelte';
  import IconTile from '../../shared/ui/tile/IconTile.svelte';
  import type { AdminMessageKey } from '../messages/catalog.ts';
  import { getAdminTranslator } from '../messages/translator-context.ts';
  import type { AdminArea, Notice, Stage } from '../session/admin-area.svelte.ts';
  import type { Problem } from '../session/problems.ts';
  import CommandLine from './CommandLine.svelte';

  interface Props {
    readonly area: AdminArea;
  }

  type AccessStage = Exclude<Stage, 'loading' | 'signedIn'>;

  interface Screen {
    readonly icon: IconName;
    readonly tone: 'current' | 'neutral' | 'warning';
    readonly title: AdminMessageKey;
    readonly text?: AdminMessageKey;
  }

  const SCREENS = {
    unconfigured: {
      icon: 'terminal',
      tone: 'neutral',
      title: 'access.unconfigured.title',
      text: 'access.unconfigured.text',
    },
    enroll: { icon: 'key', tone: 'current', title: 'access.enroll.title', text: 'access.enroll.text' },
    signIn: { icon: 'lock', tone: 'current', title: 'access.signIn.title', text: 'access.signIn.text' },
    unsupported: {
      icon: 'caution',
      tone: 'warning',
      title: 'access.unsupported.title',
      text: 'access.unsupported.text',
    },
    insecure: { icon: 'caution', tone: 'warning', title: 'access.insecure.title', text: 'access.insecure.text' },
    unavailable: { icon: 'caution', tone: 'warning', title: 'access.unavailable.title' },
  } as const satisfies Record<AccessStage, Screen>;

  const COMMANDS = [
    { label: 'access.unconfigured.binary', command: 'nylzo admin-enroll' },
    { label: 'access.unconfigured.docker', command: 'docker compose exec -T nylzo /ko-app/nylzo admin-enroll' },
  ] as const;

  const NOTICES = {
    enrolled: 'access.notice.enrolled',
    ended: 'access.notice.ended',
    signedOut: 'access.notice.signedOut',
  } as const satisfies Record<Notice, AdminMessageKey>;

  const PROBLEMS = {
    cancelled: 'access.problem.cancelled',
    'not-allowed': 'access.problem.not-allowed',
    unsupported: 'access.problem.unsupported',
    'key-type': 'access.problem.key-type',
    'already-enrolled': 'access.problem.already-enrolled',
    link: 'access.problem.link',
    missing: 'access.problem.missing',
    refused: 'access.problem.refused',
    busy: 'access.problem.busy',
    offline: 'access.problem.offline',
    failed: 'access.problem.failed',
  } as const satisfies Record<Problem, AdminMessageKey>;

  const { area }: Props = $props();
  const { translate } = getAdminTranslator();
  const screen: Screen | undefined = $derived(
    area.stage === 'loading' || area.stage === 'signedIn' ? undefined : SCREENS[area.stage],
  );
</script>

<section
  aria-labelledby="access-title"
  class="glass mx-auto max-w-column animate-rise rounded-card p-6 [animation-delay:80ms] md:p-10"
>
  {#if screen === undefined}
    <h1 id="access-title" class="sr-only">{translate('access.area')}</h1>
    <StateCard loading={translate('access.loading')} />
  {:else}
    <IconTile icon={screen.icon} tone={screen.tone} />
    <h1
      id="access-title"
      tabindex="-1"
      aria-describedby={area.notice && area.stage === 'signIn' ? 'access-notice' : undefined}
      class="mt-5 font-display text-page font-light text-balance text-ink outline-none md:text-page-lg"
      {@attach (heading) => {
        if (area.notice === 'ended' || area.notice === 'signedOut') {
          heading.focus({ preventScroll: true });
        }
      }}
    >
      {translate(screen.title)}
    </h1>
    {#if screen.text}
      <p class="mt-2 text-body text-pretty text-ink-soft">{translate(screen.text)}</p>
    {/if}
    {#if area.notice && area.stage === 'signIn'}
      <p id="access-notice" role="status" class="mt-5 flex animate-appear items-center gap-2.5 text-body-sm text-ink">
        <Icon name="check" class="size-4 shrink-0 text-aegis-300" stroke={2} />
        <span>{translate(NOTICES[area.notice])}</span>
      </p>
    {/if}
    {#if area.stage === 'unconfigured'}
      <div class="mt-6 grid gap-4">
        {#each COMMANDS as line (line.command)}
          <CommandLine label={translate(line.label)} command={line.command} />
        {/each}
      </div>
      <p class="mt-5 flex items-start gap-2 text-caption text-pretty text-ink-mute">
        <Icon name="info" class="mt-0.5 size-4 shrink-0" />
        <span>{translate('access.unconfigured.counting')}</span>
      </p>
    {:else if area.stage === 'enroll' || area.stage === 'signIn'}
      <div class="mt-7 flex flex-wrap items-center gap-4">
        <Button
          variant="primary"
          size="large"
          disabled={area.busy}
          aria-busy={area.busy}
          onclick={() => void (area.stage === 'enroll' ? area.enroll() : area.signIn())}
        >
          <Icon name="key" class="size-4.5" />
          {translate(area.stage === 'enroll' ? 'access.enroll.action' : 'access.signIn.action')}
        </Button>
        <p role="status" class="text-body-sm text-ink-soft">{area.busy ? translate('access.waiting') : ''}</p>
      </div>
    {:else if area.stage === 'unavailable'}
      <Button variant="tint" class="mt-7" onclick={() => void area.start()}>
        <Icon name="refresh" />{translate('access.retry')}
      </Button>
    {/if}
    {#if area.problem}
      <ServiceCallout class="mt-6 animate-appear" message={translate(PROBLEMS[area.problem])} />
    {/if}
  {/if}
</section>
