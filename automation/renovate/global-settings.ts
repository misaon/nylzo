export interface GlobalSettingsInput {
  readonly affectedPackages: readonly string[];
  readonly maintenanceBranch: string | null;
  readonly miseVersion: string;
  readonly bunVersion: string;
}

export function globalSettings(input: GlobalSettingsInput): Record<string, unknown> {
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
    constraints: { mise: input.miseVersion, bun: input.bunVersion },
    packageRules: fastPathRules,
    force: {
      baseBranchPatterns: input.maintenanceBranch === null ? ['main'] : ['main', input.maintenanceBranch],
    },
  };
}
