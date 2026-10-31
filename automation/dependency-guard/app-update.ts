import { repositoryName } from '../github/client.ts';
import { type BotIdentity, isBot } from '../github/identity.ts';
import { compareVersions, isSafeUpgrade, parseVersion, type SemanticVersion } from '../versions/semantic-version.ts';
import { binaryAssetsPath, hostToolchainPath, ruleFor } from './rules/dependency-files.ts';
import { type FileReview, mergeReviews, type VersionUpdate } from './rules/file-review.ts';
import { toolchainMismatches } from './rules/manifests.ts';
import {
  type Commit,
  type GuardSource,
  isRegularFile,
  type PullRequest,
  readFileAt,
  type Tree,
  webFlow,
} from './source.ts';
import { upstreamProblems } from './upstream.ts';

export interface AppUpdate {
  readonly baseHead: string;
  readonly bot: BotIdentity;
  readonly commits: readonly Commit[];
  readonly pull: PullRequest;
  readonly source: GuardSource;
}

const maximumChangedFiles = 80;
const supportedBase = /^(?:main|release\/v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))$/;

function stableVersion(value: string): SemanticVersion | undefined {
  try {
    return parseVersion(value);
  } catch {
    return undefined;
  }
}

export function upgradeProblem(update: VersionUpdate): string | undefined {
  const before = stableVersion(update.before);
  const after = stableVersion(update.after);
  const change = `${update.name}: ${update.before} → ${update.after}`;
  if (before === undefined || after === undefined) {
    return `${change} is not a stable semantic version`;
  }
  if (isSafeUpgrade(before, after)) {
    return undefined;
  }
  if (compareVersions(after, before) <= 0) {
    return `${change} is not newer`;
  }
  return after.major === before.major
    ? `${change} changes the minor version below 1.0`
    : `${change} changes the major version`;
}

function madeByApp(commit: Commit, bot: BotIdentity): boolean {
  return isBot(commit.author, bot) && (isBot(commit.committer, bot) || isBot(commit.committer, webFlow));
}

function pullRequestProblems({ baseHead, bot, commits, pull }: AppUpdate): string[] {
  const foreign = commits.filter((commit) => !madeByApp(commit, bot));
  if (foreign.length > 0) {
    return foreign.map((commit) => `Commit ${commit.sha} is not authored and committed by the automation App.`);
  }
  const problems: string[] = [];
  if (pull.headRepository !== repositoryName || pull.baseRepository !== repositoryName) {
    problems.push(`The pull request comes from ${pull.headRepository ?? 'a deleted repository'}.`);
  }
  if (!supportedBase.test(pull.baseBranch)) {
    problems.push(`The pull request targets ${pull.baseBranch}, not main or a release/vX.Y branch.`);
  }
  if (pull.draft) {
    problems.push('The pull request is a draft.');
  }
  const [commit] = commits;
  if (commit === undefined || commits.length !== 1 || pull.commitCount !== 1 || commit.sha !== pull.head) {
    problems.push(`The pull request has ${pull.commitCount} commits; an automatic update has exactly one.`);
    return problems;
  }
  if (!commit.verified) {
    problems.push(`Commit ${commit.sha} has no verified signature.`);
  }
  if (commit.parents.length !== 1 || commit.parents[0] !== baseHead) {
    problems.push(
      `Rebase required: commit ${commit.sha} does not sit on the current ${pull.baseBranch} head ${baseHead}.`,
    );
  }
  return problems;
}

function changedFiles(before: Tree, after: Tree): { paths: string[]; problems: string[] } {
  const paths: string[] = [];
  const problems: string[] = [];
  const allPaths = new Set([...before.keys(), ...after.keys()]);
  for (const path of allPaths) {
    const old = before.get(path);
    const next = after.get(path);
    if (old?.sha === next?.sha && old?.mode === next?.mode && old?.type === next?.type) {
      continue;
    }
    if (old?.type === 'tree' && next?.type === 'tree') {
      continue;
    }
    if (old === undefined) {
      problems.push(`Adds ${path}.`);
    } else if (next === undefined) {
      problems.push(`Removes ${path}.`);
    } else if (isRegularFile(old) && isRegularFile(next) && old.mode === next.mode) {
      paths.push(path);
    } else {
      problems.push(`Changes the type or mode of ${path}.`);
    }
  }
  return { paths, problems };
}

async function reviewFiles(
  source: GuardSource,
  paths: readonly string[],
  before: Tree,
  after: Tree,
): Promise<FileReview> {
  const reviews: FileReview[] = [];
  for (const path of paths) {
    const rule = ruleFor(path);
    const old = before.get(path);
    const next = after.get(path);
    if (rule === undefined || old === undefined || next === undefined) {
      reviews.push({ claims: [], findings: [`${path} is not a dependency file.`], updates: [] });
      continue;
    }
    const [oldText, newText] = await Promise.all([source.blob(old.sha), source.blob(next.sha)]);
    reviews.push(rule(path, oldText, newText));
  }
  return mergeReviews(reviews);
}

export async function appUpdateProblems(update: AppUpdate): Promise<string[]> {
  const problems = pullRequestProblems(update);
  if (problems.length > 0) {
    return problems;
  }
  const { baseHead, pull, source } = update;
  const [before, after] = await Promise.all([source.tree(baseHead), source.tree(pull.head)]);
  const changes = changedFiles(before, after);
  if (changes.paths.length + changes.problems.length === 0) {
    return ['The pull request changes no files.'];
  }
  if (changes.paths.length + changes.problems.length > maximumChangedFiles) {
    return [`The pull request changes more than ${maximumChangedFiles} files.`];
  }
  const review = await reviewFiles(source, changes.paths, before, after);
  problems.push(...changes.problems, ...review.findings);
  for (const versionUpdate of review.updates) {
    const problem = upgradeProblem(versionUpdate);
    if (problem !== undefined) {
      problems.push(problem);
    }
  }
  if (changes.paths.includes(hostToolchainPath)) {
    const [toolchain = '', assets = ''] = await Promise.all([
      readFileAt(source, pull.head, hostToolchainPath),
      readFileAt(source, pull.head, binaryAssetsPath),
    ]);
    problems.push(...toolchainMismatches(toolchain, assets));
  }
  problems.push(...(await upstreamProblems(source, review.claims)));
  return problems;
}
