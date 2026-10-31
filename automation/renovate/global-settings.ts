export interface GlobalSettingsInput {
  readonly affectedPackages: readonly string[];
  readonly maintenanceBranch: string | null;
  readonly miseVersion: string;
}

export function globalSettings(input: GlobalSettingsInput): Record<string, unknown> {
  const maintenanceRules =
    input.maintenanceBranch === null
      ? []
      : [
          {
            description: 'The maintenance branch takes only security fixes.',
            matchBaseBranches: [input.maintenanceBranch],
            matchPackageNames: ['*'],
            enabled: false,
          },
        ];
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
    allowedCommands: [String.raw`^node /opt/nylzo-dependency-sync\.ts$`],
    allowShellExecutorForPostUpgradeCommands: false,
    cacheDir: '/tmp/renovate-cache',
    baseDir: '/tmp/renovate-work',
    persistRepoData: false,
    constraints: { mise: input.miseVersion },
    packageRules: [...maintenanceRules, ...fastPathRules],
    force: {
      baseBranchPatterns: input.maintenanceBranch === null ? ['main'] : ['main', input.maintenanceBranch],
    },
  };
}
