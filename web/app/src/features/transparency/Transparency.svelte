<script lang="ts">
  import { createApi, type Transparency } from '../../shared/api/api.ts';
  import { BUILD_INFO } from '../../shared/build-info.ts';
  import { formatBytes, formatDays, formatDuration } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../shared/shell/repository.ts';
  import {
    PAGE_ALERT,
    PAGE_BUTTON,
    PAGE_CODE,
    PAGE_FACT_DETAIL,
    PAGE_FACT_TERM,
    PAGE_FACTS,
    PAGE_HEADING,
    PAGE_LINK,
    PAGE_LIST,
    PAGE_LIST_ITEM,
    PAGE_STATUS,
    PAGE_TEXT,
    PAGE_TITLE,
  } from '../../shared/ui/page-classes.ts';
  import { contactHref } from './transparency.ts';

  type View =
    | { readonly view: 'loading' }
    | { readonly view: 'failed' }
    | { readonly view: 'ready'; readonly data: Transparency };

  const DAY_SECONDS = 86_400;
  const HEADING = ['mt-8', PAGE_HEADING];
  const CODE = [PAGE_CODE, 'wrap-anywhere'];

  const translator = getTranslator();
  const { translate } = translator;
  const api = createApi();

  let view = $state.raw<View>({ view: 'loading' });
  const data = $derived(view.view === 'ready' ? view.data : undefined);
  const operator = $derived(data?.operator);
  const contactLink = $derived(operator?.contact === undefined ? undefined : contactHref(operator.contact));

  const days = (seconds: number) => formatDays(Math.round(seconds / DAY_SECONDS), translator.language);
  const period = (seconds: number) =>
    seconds % DAY_SECONDS === 0 ? days(seconds) : formatDuration(seconds, translator.language);
  const limit = (value: number) => (value === 0 ? translate('transparency.limits.none') : String(value));

  function verifyCommand(): string {
    return `nylzo verify-client --server ${location.origin}`;
  }

  async function load() {
    view = { view: 'loading' };
    try {
      view = { view: 'ready', data: await api.transparency() };
    } catch {
      view = { view: 'failed' };
    }
  }

  void load();
</script>

<section aria-labelledby="transparency-title">
  <h2 id="transparency-title" class={PAGE_TITLE}>{translate('transparency.title')}</h2>
  <p class={PAGE_TEXT}>{translate('transparency.intro')}</p>

  <h3 class={HEADING}>{translate('transparency.client.title')}</h3>
  <dl class={PAGE_FACTS}>
    <dt class={PAGE_FACT_TERM}>{translate('transparency.client.version')}</dt>
    <dd data-testid="client-version" class={PAGE_FACT_DETAIL}><code class={CODE}>{BUILD_INFO.version}</code></dd>
    <dt class={PAGE_FACT_TERM}>{translate('transparency.client.commit')}</dt>
    <dd data-testid="client-commit" class={PAGE_FACT_DETAIL}><code class={CODE}>{BUILD_INFO.commit}</code></dd>
  </dl>

  {#if view.view === 'loading'}
    <p role="status" class={PAGE_STATUS}>{translate('transparency.loading')}</p>
  {:else if view.view === 'failed'}
    <p role="alert" class={PAGE_ALERT}>{translate('transparency.failed')}</p>
    <button type="button" class={PAGE_BUTTON} onclick={load}>{translate('action.retry')}</button>
  {:else if data}
    <h3 class={HEADING}>{translate('transparency.operator.title')}</h3>
    {#if operator}
      <dl class={PAGE_FACTS}>
        {#if operator.name}
          <dt class={PAGE_FACT_TERM}>{translate('transparency.operator.name')}</dt>
          <dd class={PAGE_FACT_DETAIL}>{operator.name}</dd>
        {/if}
        {#if operator.contact}
          <dt class={PAGE_FACT_TERM}>{translate('transparency.operator.contact')}</dt>
          <dd class={PAGE_FACT_DETAIL}>
            {#if contactLink}
              <a href={contactLink} rel="noreferrer" class={[PAGE_LINK, 'wrap-anywhere']}>{operator.contact}</a>
            {:else}
              {operator.contact}
            {/if}
          </dd>
        {/if}
        {#if operator.hosting}
          <dt class={PAGE_FACT_TERM}>{translate('transparency.operator.hosting')}</dt>
          <dd class={PAGE_FACT_DETAIL}>{operator.hosting}</dd>
        {/if}
      </dl>
    {:else}
      <p class={PAGE_TEXT}>{translate('transparency.operator.missing')}</p>
    {/if}

    <h3 class={HEADING}>{translate('transparency.stores.title')}</h3>
    <ul class={PAGE_LIST}>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.stores.content')}</li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.stores.metadata')}</li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.stores.keys')}</li>
      <li class={PAGE_LIST_ITEM}>
        {translate('transparency.stores.ended', { days: days(data.retention.tombstoneSeconds) })}
      </li>
      <li class={PAGE_LIST_ITEM}>
        {translate('transparency.stores.abuse', { days: days(data.retention.abuseReportSeconds) })}
      </li>
      <li class={PAGE_LIST_ITEM}>
        {translate('transparency.stores.counters', { days: days(data.retention.dailyCounterSeconds) })}
      </li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.stores.addresses')}</li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.stores.enforced')}</li>
    </ul>

    <h3 class={HEADING}>{translate('transparency.logs.title')}</h3>
    <ul class={PAGE_LIST}>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.logs.what')}</li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.logs.never')}</li>
    </ul>

    <h3 class={HEADING}>{translate('transparency.others.title')}</h3>
    <ul class={PAGE_LIST}>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.others.hosting')}</li>
      <li class={PAGE_LIST_ITEM}>{translate('transparency.others.client')}</li>
    </ul>

    <h3 class={HEADING}>{translate('transparency.limits.title')}</h3>
    <dl class={PAGE_FACTS}>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.file')}</dt>
      <dd class={PAGE_FACT_DETAIL}>{formatBytes(data.limits.maxFileBytes, translator.language)}</dd>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.ttl')}</dt>
      <dd class={PAGE_FACT_DETAIL}>{period(data.limits.maxTtlSeconds)}</dd>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.opens')}</dt>
      <dd class={PAGE_FACT_DETAIL}>{data.limits.maxOpens}</dd>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.create')}</dt>
      <dd class={PAGE_FACT_DETAIL}>
        {translate('transparency.limits.create.value', {
          hour: limit(data.rates.createPerHour),
          day: limit(data.rates.createPerDay),
        })}
      </dd>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.read')}</dt>
      <dd class={PAGE_FACT_DETAIL}>
        {translate('transparency.limits.read.value', { hour: limit(data.rates.readPerHour) })}
      </dd>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.limits.global')}</dt>
      <dd class={PAGE_FACT_DETAIL}>
        {data.rates.globalDailyCreates === 0
          ? translate('transparency.limits.none')
          : translate('transparency.limits.global.value', { max: data.rates.globalDailyCreates })}
      </dd>
    </dl>

    <h3 class={HEADING}>{translate('transparency.server.title')}</h3>
    <dl class={PAGE_FACTS}>
      <dt class={PAGE_FACT_TERM}>{translate('transparency.server.version')}</dt>
      <dd data-testid="server-version" class={PAGE_FACT_DETAIL}><code class={CODE}>{data.server}</code></dd>
    </dl>
    <p class={PAGE_TEXT}>{translate('transparency.client.files')}</p>
    <ul class={PAGE_LIST}>
      {#each data.files as file (file.path)}
        <li class={PAGE_LIST_ITEM}><code class={CODE}>{file.path}</code> <code class={CODE}>{file.sha256}</code></li>
      {/each}
    </ul>
    <p class={PAGE_TEXT}>{translate('transparency.client.verify')}</p>
    <p class={PAGE_TEXT}><code class={CODE}>{verifyCommand()}</code></p>
    <p class={PAGE_TEXT}>{translate('transparency.client.limit')}</p>
    <p class={PAGE_TEXT}>{translate('transparency.client.independent')}</p>
    <p class={PAGE_TEXT}>
      <a href={REPOSITORY_URL} rel="noreferrer" class={PAGE_LINK}>{translate('transparency.source')}</a>
    </p>
  {/if}
</section>
