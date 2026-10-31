import assert from "node:assert/strict";
import { deepmergePatch } from "./deepmerge.mts";

export const platforms = ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"];

export function assess(report: any, policy: any, imports: Record<string, string[]>, now = new Date(), verifiedPatches: any[] = []) {
  assert(Array.isArray(report.results) && report.results.length === 3, "Application and release-tooling lockfiles must all be scanned");
  const paths = report.results.map((result: any) => result.source?.path?.replaceAll("\\", "/"));
  assert(paths.some((path: string) => path.endsWith("/go.mod")) && paths.some((path: string) => path.endsWith("/web/pnpm-lock.yaml")));
  assert(paths.some((path: string) => path.endsWith("/automation/pnpm-lock.yaml")));
  const allowed = new Set(policy.allowedLicenses);
  assert(allowed.size > 0);
  const findings: string[] = [], statements: any[] = [];
  for (const result of report.results) {
    const licenses = result.source.path.replaceAll("\\", "/").endsWith("/automation/pnpm-lock.yaml") ? new Set([...allowed, ...(policy.toolingAllowedLicenses ?? [])]) : allowed;
    assert(Array.isArray(result.packages) && result.packages.length > 0, "Empty dependency inventory");
    for (const entry of result.packages) {
      const component = entry.package;
      assert(component?.name && component?.version && component?.ecosystem);
      if (!Array.isArray(entry.licenses) || entry.licenses.length === 0 || entry.licenses.some((license: string) => !licenses.has(license)) || entry.license_violations?.length) {
        findings.push(`License review required: ${component.name}@${component.version}`);
      }
      for (const vulnerability of entry.vulnerabilities ?? []) {
        assert(typeof vulnerability.id === "string");
        const identifiers = [vulnerability.id, ...(vulnerability.aliases ?? [])];
        if (identifiers.some(id => id.startsWith("MAL-"))) { findings.push(`Known malware: ${component.name} ${vulnerability.id}`); continue; }
        const fixed = result.source.path.replaceAll("\\", "/").endsWith("/web/pnpm-lock.yaml") && policy.fixed?.find((candidate: any) => candidate.id === vulnerability.id && candidate.package === component.name && candidate.version === component.version && candidate.ecosystem === component.ecosystem);
        if (fixed) {
          for (const [key, value] of Object.entries(deepmergePatch)) assert.equal(fixed[key === "sha256" ? "patchSha256" : key], value, "Only the reviewed deepmerge remediation is supported");
          assert(Number.isFinite(Date.parse(fixed.reviewUntil)) && now.getTime() < Date.parse(fixed.reviewUntil), "Patch assessment expired");
          assert(verifiedPatches.some(patch => Object.entries(deepmergePatch).every(([key, value]) => patch[key] === value)), "Missing installed patch and regression evidence");
          statements.push({ vulnerability: { name: vulnerability.id }, status: "fixed", impact_statement: fixed.impact, component: `pkg:npm/${component.name}@${component.version}` });
          continue;
        }
        const exception = policy.notAffected.find((candidate: any) => candidate.id === vulnerability.id && candidate.package === component.name && candidate.version === component.version && candidate.ecosystem === component.ecosystem);
        if (!exception) { findings.push(`Vulnerability: ${component.name}@${component.version} ${vulnerability.id}`); continue; }
        assert.equal(exception.justification, "vulnerable_code_not_present");
        assert(Number.isFinite(Date.parse(exception.reviewUntil)) && now.getTime() < Date.parse(exception.reviewUntil), "VEX assessment expired");
        assert.equal(exception.forbiddenImport, "golang.org/x/crypto/openpgp");
        assert.deepEqual(Object.keys(imports).sort(), [...platforms].sort(), "All release targets need import evidence");
        for (const platform of platforms) {
          assert(Array.isArray(imports[platform]) && imports[platform]!.includes("github.com/misaon/nylzo/cmd/nylzo"), "Missing complete application import inventory");
          assert(!imports[platform]!.some(path => path === exception.forbiddenImport || path.startsWith(`${exception.forbiddenImport}/`)), `Vulnerable OpenPGP code is present for ${platform}`);
        }
        statements.push({ vulnerability: { name: vulnerability.id }, status: "not_affected", justification: exception.justification, impact_statement: exception.impact, component: `pkg:golang/${component.name}@v${component.version}` });
      }
    }
  }
  assert.equal(findings.length, 0, findings.join("\n"));
  return statements;
}
