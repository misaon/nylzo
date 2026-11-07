import type { MessageKey } from '../../shared/language/czech.ts';
import { REPOSITORY_URL } from '../../shared/shell/repository.ts';

export interface AgentLink {
  readonly label: MessageKey;
  readonly href: string;
}

export interface AgentStep {
  readonly title: MessageKey;
  readonly text: MessageKey;
  readonly link?: AgentLink;
}

export const AGENT_STEPS: readonly AgentStep[] = [
  {
    title: 'agents.install.title',
    text: 'agents.install.text',
    link: { label: 'agents.install.link', href: `${REPOSITORY_URL}/releases/latest` },
  },
  {
    title: 'agents.skill.title',
    text: 'agents.skill.text',
    link: { label: 'agents.skill.link', href: `${REPOSITORY_URL}/tree/main/skills/nylzo` },
  },
  { title: 'agents.ask.title', text: 'agents.ask.text' },
];

export const AGENT_RULES = ['agents.rules.1', 'agents.rules.2', 'agents.rules.3', 'agents.rules.4'] as const;

export const DISCUSSIONS: AgentLink = { label: 'agents.missing.link', href: `${REPOSITORY_URL}/discussions` };

export function agentsGuide(translate: (key: MessageKey) => string): string {
  const linkLine = ({ label, href }: AgentLink) => `[${translate(label)}](${href})`;
  const steps = AGENT_STEPS.flatMap(({ title, text, link }, index) => {
    const lines = [`## ${index + 1}. ${translate(title)}`, '', translate(text), ''];
    if (link) {
      lines.push(linkLine(link), '');
    }
    return lines;
  });
  return [
    `# ${translate('agents.title')}`,
    '',
    translate('agents.lead'),
    '',
    ...steps,
    `## ${translate('agents.rules.title')}`,
    '',
    ...AGENT_RULES.map((rule) => `- ${translate(rule)}`),
    '',
    `## ${translate('agents.missing.title')}`,
    '',
    translate('agents.missing.text'),
    '',
    linkLine(DISCUSSIONS),
    '',
  ]
    .join('\n')
    .replaceAll('\u{A0}', ' ');
}
