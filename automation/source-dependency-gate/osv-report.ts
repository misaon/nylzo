import { arrayAt, objectAt, optionalArrayAt, textAt, textsAt } from './json-values.ts';

export interface Component {
  readonly ecosystem: string;
  readonly name: string;
  readonly version: string;
}

export interface DependencyReport {
  readonly results: readonly LockfileReport[];
}

export interface LockedPackage {
  readonly component: Component;
  readonly licenses: readonly string[];
  readonly licenseViolations: number;
  readonly vulnerabilities: readonly Vulnerability[];
}

export interface LockfileReport {
  readonly packages: readonly LockedPackage[];
  readonly path: string;
}

export interface Vulnerability {
  readonly aliases: readonly string[];
  readonly id: string;
}

export function parseDependencyReport(value: unknown): DependencyReport {
  const results = arrayAt(objectAt(value, 'report')['results'], 'report.results');
  return { results: results.map((result, index) => parseLockfileReport(result, `report.results[${index}]`)) };
}

export function lockedPackageCount(report: DependencyReport): number {
  return report.results.flatMap((result) => result.packages).length;
}

function parseLockfileReport(value: unknown, location: string): LockfileReport {
  const result = objectAt(value, location);
  const source = objectAt(result['source'], `${location}.source`);
  const packages = arrayAt(result['packages'], `${location}.packages`);
  const path = textAt(source['path'], `${location}.source.path`).replaceAll('\\', '/');
  return {
    packages: packages.flatMap((entry, index) => {
      const parsed = parseLockedPackage(entry, `${location}.packages[${index}]`, path);
      return parsed === undefined ? [] : [parsed];
    }),
    path,
  };
}

function parseLockedPackage(value: unknown, location: string, lockfile: string): LockedPackage | undefined {
  const entry = objectAt(value, location);
  const component = objectAt(entry['package'], `${location}.package`);
  const vulnerabilities = optionalArrayAt(entry['vulnerabilities'], `${location}.vulnerabilities`);
  const licenseViolations = optionalArrayAt(entry['license_violations'], `${location}.license_violations`);
  const licenses = textsAt(optionalArrayAt(entry['licenses'], `${location}.licenses`), `${location}.licenses`);
  if (
    lockfile.endsWith('/deploy/caddy/go.mod') &&
    component['ecosystem'] === 'Go' &&
    component['name'] === '../..' &&
    component['version'] === ''
  ) {
    // This exact replacement is the repository root; its external dependencies are scanned through go.mod.
    if (vulnerabilities.length > 0 || licenseViolations.length > 0) {
      throw new TypeError(`${location}: repository source reference has security or license findings`);
    }
    return undefined;
  }
  return {
    component: {
      ecosystem: textAt(component['ecosystem'], `${location}.package.ecosystem`),
      name: textAt(component['name'], `${location}.package.name`),
      version: textAt(component['version'], `${location}.package.version`),
    },
    licenses,
    licenseViolations: licenseViolations.length,
    vulnerabilities: vulnerabilities.map((vulnerability, index) =>
      parseVulnerability(vulnerability, `${location}.vulnerabilities[${index}]`),
    ),
  };
}

function parseVulnerability(value: unknown, location: string): Vulnerability {
  const vulnerability = objectAt(value, location);
  return {
    aliases: textsAt(optionalArrayAt(vulnerability['aliases'], `${location}.aliases`), `${location}.aliases`),
    id: textAt(vulnerability['id'], `${location}.id`),
  };
}
