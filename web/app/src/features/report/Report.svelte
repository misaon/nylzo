<script lang="ts">
  import ReportForm from '../../shared/report/ReportForm.svelte';
  import { createApi } from '../../shared/api/api.ts';
  import { getI18n } from '../../shared/i18n/i18n.svelte.ts';
  import { reportLink } from '../../shared/report/report.ts';

  const { t } = getI18n();
  const api = createApi();
</script>

<section aria-labelledby="report-title">
  <h2 id="report-title">{t('report.title')}</h2>
  <p>{t('report.note')}</p>
  <p>{t('report.privacy')}</p>
  <ReportForm name="report" withLink send={(link, reason) => reportLink(api, location.origin, link, reason)} />
</section>
