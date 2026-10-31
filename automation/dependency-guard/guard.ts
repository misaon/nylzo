import { type BotIdentity, isBot } from '../github/identity.ts';
import { appUpdateProblems } from './app-update.ts';
import { approvingCodeOwner, type CodeOwners, isCodeOwner, parseCodeOwners } from './code-owners.ts';
import { type Commit, type GuardSource, type PullRequest, readFileAt, webFlow } from './source.ts';

export interface Verdict {
  readonly passed: boolean;
  readonly reasons: readonly string[];
}

export interface Subject {
  readonly head: string;
  readonly number: number;
}

const codeOwnersPath = '.github/CODEOWNERS';

function ownerCommitProblems(commits: readonly Commit[], pull: PullRequest, owners: CodeOwners): string[] {
  const problems = commits
    .filter((commit) => {
      const committer = isBot(commit.committer, webFlow) || isCodeOwner(commit.committer, owners);
      return !commit.verified || !isCodeOwner(commit.author, owners) || !committer;
    })
    .map((commit) => `Commit ${commit.sha} is not a signed commit of a code owner.`);
  if (commits.length !== pull.commitCount) {
    problems.push(`GitHub listed ${commits.length} of ${pull.commitCount} commits.`);
  }
  return problems;
}

interface Context {
  readonly baseHead: string;
  readonly bot: BotIdentity;
  readonly owners: CodeOwners;
  readonly pull: PullRequest;
  readonly source: GuardSource;
}

async function automaticVerdict({
  baseHead,
  bot,
  owners,
  pull,
  source,
}: Context): Promise<{ passed: string } | { problems: string[] }> {
  const commits = await source.commits(pull.number);
  if (isCodeOwner(pull.author, owners)) {
    const problems = ownerCommitProblems(commits, pull, owners);
    return problems.length === 0 ? { passed: `Authored by code owner @${pull.author.login}.` } : { problems };
  }
  if (isBot(pull.author, bot)) {
    const problems = await appUpdateProblems({ baseHead, bot, commits, pull, source });
    return problems.length === 0
      ? { passed: 'A verified, safe dependency update from the automation App.' }
      : { problems };
  }
  return { problems: [`@${pull.author.login} is neither a code owner nor the automation App.`] };
}

export async function decide(source: GuardSource, bot: BotIdentity, subject: Subject): Promise<Verdict> {
  const pull = await source.pullRequest(subject.number);
  if (pull.head !== subject.head) {
    return {
      passed: false,
      reasons: [`The pull request moved from ${subject.head} to ${pull.head}; the run for the new head decides.`],
    };
  }
  const baseHead = await source.branchHead(pull.baseBranch);
  const owners = parseCodeOwners((await readFileAt(source, baseHead, codeOwnersPath)) ?? '');
  const verdict = await automaticVerdict({ baseHead, bot, owners, pull, source });
  if ('passed' in verdict) {
    return { passed: true, reasons: [verdict.passed] };
  }
  const approver = approvingCodeOwner(await source.reviews(pull.number), owners, pull.head);
  if (approver !== undefined) {
    return { passed: true, reasons: [...verdict.problems, `Code owner @${approver} approved ${pull.head}.`] };
  }
  return {
    passed: false,
    reasons: [...verdict.problems, `A code owner has to approve ${pull.head} and then re-run this check.`],
  };
}
