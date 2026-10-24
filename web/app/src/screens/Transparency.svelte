<script lang="ts">
  import { createApi, type Transparency } from '../lib/api.ts';
  import { formatBytes, formatDays, formatDuration } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { contactHref, verifyCommand } from '../lib/transparency.ts';

  type View = { readonly view: 'loading' } | { readonly view: 'failed' } | { readonly view: 'ready'; readonly data: Transparency };

  const SOURCE_URL = 'https://github.com/misaon/nylzo';
  const DAY_SECONDS = 86_400;

  const i18n = getI18n();
  const { t } = i18n;
  const api = createApi();

  let view = $state.raw<View>({ view: 'loading' });

  const days = (seconds: number) => formatDays(Math.round(seconds / DAY_SECONDS), i18n.language);
  const period = (seconds: number) => (seconds % DAY_SECONDS === 0 ? days(seconds) : formatDuration(seconds, i18n.language));
  const limit = (value: number) => (value === 0 ? t('transparency.limits.none') : String(value));

  async function load() {
    view = { view: 'loading' };
    try {
      view = { view: 'ready', data: await api.transparency() };
    } catch {
      view = { view: 'failed' };
    }
  }

  load();
</script>

<section aria-labelledby="transparency-title">
  <h2 id="transparency-title">{t('transparency.title')}</h2>
  <p>{t('transparency.intro')}</p>

  {#if view.view === 'loading'}
    <p role="status">{t('transparency.loading')}</p>
  {:else if view.view === 'failed'}
    <p role="alert">{t('transparency.failed')}</p>
    <button type="button" onclick={load}>{t('action.retry')}</button>
  {:else}
    {@const data = view.data}
    {@const operator = data.operator}
    {@const href = operator?.contact === undefined ? undefined : contactHref(operator.contact)}

    <h3>{t('transparency.operator.title')}</h3>
    {#if operator}
      <dl>
        {#if operator.name}
          <dt>{t('transparency.operator.name')}</dt>
          <dd>{operator.name}</dd>
        {/if}
        {#if operator.contact}
          <dt>{t('transparency.operator.contact')}</dt>
          <dd>
            {#if href}
              <a {href} rel="noreferrer" class="wrap-anywhere">{operator.contact}</a>
            {:else}
              {operator.contact}
            {/if}
          </dd>
        {/if}
        {#if operator.hosting}
          <dt>{t('transparency.operator.hosting')}</dt>
          <dd>{operator.hosting}</dd>
        {/if}
      </dl>
    {:else}
      <p>{t('transparency.operator.missing')}</p>
    {/if}

    <h3>{t('transparency.stores.title')}</h3>
    <ul>
      <li>{t('transparency.stores.content')}</li>
      <li>{t('transparency.stores.metadata')}</li>
      <li>{t('transparency.stores.keys')}</li>
      <li>{t('transparency.stores.ended', { days: days(data.retention.tombstoneSeconds) })}</li>
      <li>{t('transparency.stores.abuse', { days: days(data.retention.abuseReportSeconds) })}</li>
      <li>{t('transparency.stores.counters', { days: days(data.retention.dailyCounterSeconds) })}</li>
      <li>{t('transparency.stores.addresses')}</li>
    </ul>

    <h3>{t('transparency.logs.title')}</h3>
    <ul>
      <li>{t('transparency.logs.what')}</li>
      <li>{t('transparency.logs.never')}</li>
    </ul>

    <h3>{t('transparency.others.title')}</h3>
    <ul>
      <li>{t('transparency.others.hosting')}</li>
      <li>{t('transparency.others.client')}</li>
    </ul>

    <h3>{t('transparency.limits.title')}</h3>
    <dl>
      <dt>{t('transparency.limits.file')}</dt>
      <dd>{formatBytes(data.limits.maxFileBytes, i18n.language)}</dd>
      <dt>{t('transparency.limits.ttl')}</dt>
      <dd>{period(data.limits.maxTtlSeconds)}</dd>
      <dt>{t('transparency.limits.opens')}</dt>
      <dd>{data.limits.maxOpens}</dd>
      <dt>{t('transparency.limits.create')}</dt>
      <dd>{t('transparency.limits.create.value', { hour: limit(data.rates.createPerHour), day: limit(data.rates.createPerDay) })}</dd>
      <dt>{t('transparency.limits.read')}</dt>
      <dd>{t('transparency.limits.read.value', { hour: limit(data.rates.readPerHour) })}</dd>
      <dt>{t('transparency.limits.global')}</dt>
      <dd>
        {data.rates.globalDailyCreates === 0
          ? t('transparency.limits.none')
          : t('transparency.limits.global.value', { max: data.rates.globalDailyCreates })}
      </dd>
    </dl>

    <h3>{t('transparency.client.title')}</h3>
    <dl>
      <dt>{t('transparency.client.version')}</dt>
      <dd><code class="wrap-anywhere">{data.server}</code></dd>
    </dl>
    <p>{t('transparency.client.files')}</p>
    <ul>
      {#each data.files as file (file.path)}
        <li><code class="wrap-anywhere">{file.path}</code> <code class="wrap-anywhere">{file.sha256}</code></li>
      {/each}
    </ul>
    <p>{t('transparency.client.verify')}</p>
    <p><code class="wrap-anywhere">{verifyCommand(location.origin)}</code></p>
    <p>{t('transparency.client.limit')}</p>
    <p><a href={SOURCE_URL} rel="noreferrer">{t('transparency.source')}</a></p>
  {/if}
</section>
