import { z } from 'zod';

export interface BotIdentity {
  readonly id: number;
  readonly login: string;
}

const automationApp = z.object({
  DEPENDENCY_APP_SLUG: z.literal('nylzo-dependency-updates'),
  DEPENDENCY_BOT_ID: z.coerce.number().int().positive(),
});

export function automationBot(environment: NodeJS.ProcessEnv = process.env): BotIdentity {
  const { DEPENDENCY_APP_SLUG, DEPENDENCY_BOT_ID } = automationApp.parse(environment);
  return { id: DEPENDENCY_BOT_ID, login: `${DEPENDENCY_APP_SLUG}[bot]` };
}

export function isBot(user: { id: bigint | number; login: string } | null | undefined, bot: BotIdentity): boolean {
  return user?.id === bot.id && user.login === bot.login;
}
