export interface ActionPin {
  readonly action: string;
  readonly commit: string;
  readonly version: string;
}

export const actionPinPattern =
  /^ *(?:- )?uses: (?<action>[\w.-]+\/[\w.-]+)(?:\/[\w./-]+)?@(?<commit>[a-f\d]{40}) # (?<version>v?\d+\.\d+\.\d+)$/dgm;

export function actionPins(workflow: string): ActionPin[] {
  return Array.from(workflow.matchAll(actionPinPattern), ({ groups }) => {
    const { action = '', commit = '', version = '' } = groups ?? {};
    return { action, commit, version };
  });
}
