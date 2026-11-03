import assert from 'node:assert/strict';
import { test } from 'node:test';

import { lockedPackageCount, parseDependencyReport } from './osv-report.ts';

function fixture() {
  const local = { package: { ecosystem: 'Go', name: '../..', purl: 'pkg:golang/../..', version: '' } };
  const external = {
    licenses: ['Apache-2.0'],
    package: { ecosystem: 'Go', name: 'github.com/caddyserver/caddy/v2', version: '2.11.7' },
    vulnerabilities: [{ id: 'GO-regression-test' }],
  };
  const result = { packages: [external, local], source: { path: '/project/deploy/caddy/go.mod' } };
  return { external, local, report: { results: [result] }, result };
}

test('OSV repository-root placeholders are excluded without losing external inventory or findings', () => {
  const data = fixture();
  const report = parseDependencyReport(data.report);
  assert.equal(lockedPackageCount(report), 1);
  assert.deepEqual(report.results[0]?.packages[0], {
    component: data.external.package,
    licenses: data.external.licenses,
    licenseViolations: 0,
    vulnerabilities: [{ aliases: [], id: 'GO-regression-test' }],
  });
});

test('missing versions are rejected outside the exact Caddy repository-root reference', () => {
  const mutations: ((data: ReturnType<typeof fixture>) => void)[] = [
    (data) => {
      data.result.source.path = '/project/go.mod';
    },
    (data) => {
      data.local.package.ecosystem = 'npm';
    },
    (data) => {
      data.local.package.name = '../../outside';
    },
    (data) => {
      data.local.package.name = 'github.com/misaon/nylzo';
    },
    (data) => {
      data.external.package.version = '';
    },
  ];
  for (const mutate of mutations) {
    const data = fixture();
    mutate(data);
    assert.throws(() => parseDependencyReport(data.report), /version must be a non-empty string/u);
  }
});

test('repository-root references cannot suppress vulnerability, malware or license findings', () => {
  for (const findings of [
    { vulnerabilities: [{ id: 'GO-regression-test' }] },
    { vulnerabilities: [{ id: 'MAL-regression-test' }] },
    { license_violations: ['UNKNOWN'] },
  ]) {
    const data = fixture();
    Object.assign(data.local, findings);
    assert.throws(
      () => parseDependencyReport(data.report),
      /repository source reference has security or license findings/u,
    );
  }
});
