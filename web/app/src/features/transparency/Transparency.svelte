<script lang="ts">
  import { createApi, type Transparency } from '../../shared/api/api.ts';
  import { BUILD_INFO } from '../../shared/build-info.ts';
  import { formatBytes, formatDays, formatDuration } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../shared/shell/repository.ts';
  import { contactHref } from './transparency.ts';

  type View =
    | { readonly view: 'loading' }
    | { readonly view: 'failed' }
    | { readonly view: 'ready'; readonly data: Transparency };

  const DAY_SECONDS = 86_400;

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
  <h2 id="transparency-title">{translate('transparency.title')}</h2>
  <p>{translate('transparency.intro')}</p>

  <h3>{translate('transparency.client.title')}</h3>
  <dl>
    <dt>{translate('transparency.client.version')}</dt>
    <dd data-testid="client-version"><code class="wrap-anywhere">{BUILD_INFO.version}</code></dd>
    <dt>{translate('transparency.client.commit')}</dt>
    <dd data-testid="client-commit"><code class="wrap-anywhere">{BUILD_INFO.commit}</code></dd>
  </dl>

  {#if view.view === 'loading'}
    <p role="status">{translate('transparency.loading')}</p>
  {:else if view.view === 'failed'}
    <p role="alert">{translate('transparency.failed')}</p>
    <button type="button" onclick={load}>{translate('action.retry')}</button>
  {:else if data}
    <h3>{translate('transparency.operator.title')}</h3>
    {#if operator}
      <dl>
        {#if operator.name}
          <dt>{translate('transparency.operator.name')}</dt>
          <dd>{operator.name}</dd>
        {/if}
        {#if operator.contact}
          <dt>{translate('transparency.operator.contact')}</dt>
          <dd>
            {#if contactLink}
              <a href={contactLink} rel="noreferrer" class="wrap-anywhere">{operator.contact}</a>
            {:else}
              {operator.contact}
            {/if}
          </dd>
        {/if}
        {#if operator.hosting}
          <dt>{translate('transparency.operator.hosting')}</dt>
          <dd>{operator.hosting}</dd>
        {/if}
      </dl>
    {:else}
      <p>{translate('transparency.operator.missing')}</p>
    {/if}

    <h3>{translate('transparency.stores.title')}</h3>
    <ul>
      <li>{translate('transparency.stores.content')}</li>
      <li>{translate('transparency.stores.metadata')}</li>
      <li>{translate('transparency.stores.keys')}</li>
      <li>{translate('transparency.stores.ended', { days: days(data.retention.tombstoneSeconds) })}</li>
      <li>{translate('transparency.stores.abuse', { days: days(data.retention.abuseReportSeconds) })}</li>
      <li>{translate('transparency.stores.counters', { days: days(data.retention.dailyCounterSeconds) })}</li>
      <li>{translate('transparency.stores.addresses')}</li>
      <li>{translate('transparency.stores.enforced')}</li>
    </ul>

    <h3>{translate('transparency.logs.title')}</h3>
    <ul>
      <li>{translate('transparency.logs.what')}</li>
      <li>{translate('transparency.logs.never')}</li>
    </ul>

    <h3>{translate('transparency.others.title')}</h3>
    <ul>
      <li>{translate('transparency.others.hosting')}</li>
      <li>{translate('transparency.others.client')}</li>
    </ul>

    <h3>{translate('transparency.limits.title')}</h3>
    <dl>
      <dt>{translate('transparency.limits.file')}</dt>
      <dd>{formatBytes(data.limits.maxFileBytes, translator.language)}</dd>
      <dt>{translate('transparency.limits.ttl')}</dt>
      <dd>{period(data.limits.maxTtlSeconds)}</dd>
      <dt>{translate('transparency.limits.opens')}</dt>
      <dd>{data.limits.maxOpens}</dd>
      <dt>{translate('transparency.limits.create')}</dt>
      <dd>
        {translate('transparency.limits.create.value', {
          hour: limit(data.rates.createPerHour),
          day: limit(data.rates.createPerDay),
        })}
      </dd>
      <dt>{translate('transparency.limits.read')}</dt>
      <dd>{translate('transparency.limits.read.value', { hour: limit(data.rates.readPerHour) })}</dd>
      <dt>{translate('transparency.limits.global')}</dt>
      <dd>
        {data.rates.globalDailyCreates === 0
          ? translate('transparency.limits.none')
          : translate('transparency.limits.global.value', { max: data.rates.globalDailyCreates })}
      </dd>
    </dl>

    <h3>{translate('transparency.server.title')}</h3>
    <dl>
      <dt>{translate('transparency.server.version')}</dt>
      <dd data-testid="server-version"><code class="wrap-anywhere">{data.server}</code></dd>
    </dl>
    <p>{translate('transparency.client.files')}</p>
    <ul>
      {#each data.files as file (file.path)}
        <li><code class="wrap-anywhere">{file.path}</code> <code class="wrap-anywhere">{file.sha256}</code></li>
      {/each}
    </ul>
    <p>{translate('transparency.client.verify')}</p>
    <p><code class="wrap-anywhere">{verifyCommand()}</code></p>
    <p>{translate('transparency.client.limit')}</p>
    <p>{translate('transparency.client.independent')}</p>
    <p><a href={REPOSITORY_URL} rel="noreferrer">{translate('transparency.source')}</a></p>
  {/if}
</section>
