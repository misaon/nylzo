<script lang="ts">
  import { createApi } from '../../shared/api/api.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { instanceOrigin } from '../../shared/local-client/instance-origin.ts';
  import { reportLink, type ReportOutcome, type ReportSubmission } from '../../shared/report/report.ts';
  import ReportForm from '../../shared/report/ReportForm.svelte';
  import { PAGE_TEXT, PAGE_TITLE } from '../../shared/ui/page-classes.ts';

  const { translate } = getTranslator();
  const api = createApi();

  async function submit({ link, ...report }: ReportSubmission): Promise<ReportOutcome> {
    return reportLink(api, instanceOrigin(), link, report);
  }
</script>

<section aria-labelledby="report-title">
  <h2 id="report-title" class={PAGE_TITLE}>{translate('report.title')}</h2>
  <p class={PAGE_TEXT}>{translate('report.note')}</p>
  <p class={PAGE_TEXT}>{translate('report.privacy')}</p>
  <ReportForm name="report" withLink onsubmit={submit} />
</section>
