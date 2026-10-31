import type { UpstreamClaim } from './rules/file-review.ts';
import type { GuardSource } from './source.ts';

async function claimProblem(source: GuardSource, claim: UpstreamClaim): Promise<string | undefined> {
  switch (claim.kind) {
    case 'action-tag': {
      const commit = await source.tagCommit(claim.repository, claim.tag);
      return commit === claim.commit
        ? undefined
        : `${claim.repository}@${claim.commit} is not the commit of its tag ${claim.tag} (${commit ?? 'tag not found'})`;
    }
    case 'release-asset': {
      const digest = await source.releaseAssetDigest(claim.repository, claim.tag, claim.asset);
      return digest === claim.digest
        ? undefined
        : `${claim.repository} ${claim.tag} ${claim.asset}: ${claim.digest} is not the release asset digest (${digest ?? 'asset not found'})`;
    }
  }
}

export async function upstreamProblems(source: GuardSource, claims: readonly UpstreamClaim[]): Promise<string[]> {
  const unique = new Map(claims.map((claim) => [JSON.stringify(claim), claim]));
  const problems: string[] = [];
  for (const claim of unique.values()) {
    const problem = await claimProblem(source, claim);
    if (problem !== undefined) {
      problems.push(problem);
    }
  }
  return problems;
}
