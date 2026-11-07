export interface GlobalSettingsInput {
  readonly affectedPackages: readonly string[];
  readonly maintenanceBranch: string | null;
  readonly miseVersion: string;
  readonly bunVersion: string;
}

const maintenanceLine = String.raw`/^release\/v/`;
const maintenanceRules = [
  {
    description: 'Maintenance lines ship only updates that need no migration.',
    matchBaseBranches: [maintenanceLine],
    matchUpdateTypes: ['major'],
    enabled: false,
  },
  {
    description: 'Maintenance lines take no pre-1.0 minor either.',
    matchBaseBranches: [maintenanceLine],
    matchCurrentVersion: String.raw`/^v?0\./`,
    matchUpdateTypes: ['minor'],
    enabled: false,
  },
];

export function globalSettings(input: GlobalSettingsInput): Record<string, unknown> {
  const baseBranchPatterns = input.maintenanceBranch === null ? ['main'] : ['main', input.maintenanceBranch];
  const fastPathRules =
    input.affectedPackages.length === 0
      ? []
      : [
          {
            description: 'The vulnerability inventory found these dependencies affected; propose their fix at once.',
            matchPackageNames: [...input.affectedPackages],
            enabled: true,
            minimumReleaseAge: null,
            schedule: [],
            addLabels: ['security'],
          },
        ];
  return {
    $schema: 'https://docs.renovatebot.com/renovate-global-schema.json',
    platform: 'github',
    repositories: ['misaon/nylzo'],
    autodiscover: false,
    onboarding: false,
    requireConfig: 'required',
    forkProcessing: 'disabled',
    allowScripts: false,
    allowPlugins: false,
    exposeAllEnv: false,
    allowedUnsafeExecutions: [],
    allowedCommands: [String.raw`^(?:bun|node) /opt/nylzo-dependency-sync\.ts$`],
    allowShellExecutorForPostUpgradeCommands: false,
    cacheDir: '/tmp/renovate-cache',
    baseDir: '/tmp/renovate-work',
    persistRepoData: false,
    repositoryCache: 'enabled',
    // A repository run that ends in an error state otherwise exits 0 and the job looks green.
    exitCodeForErrors: true,
    constraints: { mise: input.miseVersion, bun: input.bunVersion },
    // Renovate applies these before the repository's rules, so the fast path must come last to win here too.
    packageRules: [...maintenanceRules, ...fastPathRules],
    baseBranchPatterns,
    force: { baseBranchPatterns },
  };
}
