import { z } from 'zod';

export interface GitObject {
  readonly sha: string;
  readonly type: string;
}

export interface TagReader {
  readonly rest: {
    readonly git: {
      getTag(parameters: {
        owner: string;
        repo: string;
        tag_sha: string;
      }): Promise<{ readonly data: { object: unknown } }>;
    };
  };
}

const gitObjectSchema = z.object({ sha: z.string().regex(/^[a-f\d]{40}$/), type: z.string() });
const nestedTagLimit = 3;

export async function peelTag(
  client: TagReader,
  repository: { readonly owner: string; readonly repo: string },
  object: GitObject,
  nestedTags = nestedTagLimit,
): Promise<GitObject> {
  let target = gitObjectSchema.parse(object);
  for (let depth = 0; target.type === 'tag' && depth < nestedTags; depth++) {
    const annotated = await client.rest.git.getTag({ ...repository, tag_sha: target.sha });
    target = gitObjectSchema.parse(annotated.data.object);
  }
  return target;
}
