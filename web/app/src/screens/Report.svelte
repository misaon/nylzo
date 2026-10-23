<script lang="ts">
  import ReportForm from '../components/ReportForm.svelte';
  import { createApi } from '../lib/api.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { reportLink } from '../lib/report.ts';

  const { t } = getI18n();
  const api = createApi();
</script>

<section aria-labelledby="report-title">
  <h2 id="report-title">{t('report.title')}</h2>
  <p>{t('report.note')}</p>
  <p>{t('report.privacy')}</p>
  <ReportForm name="report" withLink send={(link, reason) => reportLink(api, location.origin, link, reason)} />
</section>
