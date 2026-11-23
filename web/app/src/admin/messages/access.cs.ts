export const czech = {
  'access.area': 'Správa',
  'access.back': 'Zpět na web',
  'access.loading': 'Načítám správu…',
  'access.expires': 'Relace do {time}',
  'access.signOut': 'Odhlásit se',
  'access.retry': 'Zkusit znovu',
  'access.copy': 'Kopírovat',
  'access.copied': 'Zkopírováno',
  'access.copy.label': 'Kopírovat příkaz: {command}',

  'access.unconfigured.title': 'Správa zatím není nastavená',
  'access.unconfigured.text':
    'Na serveru spusťte příkaz níže a\u{A0}otevřete odkaz, který vypíše. Odkaz funguje jednou a\u{A0}platí 15\u{A0}minut.',
  'access.unconfigured.binary': 'Přímo na serveru',
  'access.unconfigured.docker': 'V\u{A0}Docker Compose',
  'access.unconfigured.counting': 'Dokud správa nemá passkey, server návštěvy ani zprávy nepočítá.',

  'access.enroll.title': 'Vytvořte passkey pro správu',
  'access.enroll.text':
    'Passkey uloží toto zařízení nebo váš správce hesel. Hned potom vás přihlásí. Odkaz z\u{A0}příkazu funguje jen jednou.',
  'access.enroll.action': 'Vytvořit passkey',

  'access.signIn.title': 'Přihlášení do správy',
  'access.signIn.text':
    'Přihlaste se passkey, který jste pro správu vytvořili. Přihlášení platí jen v\u{A0}tomto panelu, po obnovení stránky se přihlásíte znovu.',
  'access.signIn.action': 'Přihlásit se passkey',
  'access.waiting': 'Čekám na passkey…',

  'access.unsupported.title': 'Tento prohlížeč passkey neumí',
  'access.unsupported.text':
    'Správa potřebuje passkey a\u{A0}podpisy Ed25519 v\u{A0}prohlížeči. Otevřete ji v\u{A0}aktuální verzi svého prohlížeče.',
  'access.insecure.title': 'Správa potřebuje HTTPS',
  'access.insecure.text':
    'Prohlížeč dovolí passkey jen na zabezpečené adrese. Otevřete správu přes https:// na veřejné adrese serveru.',
  'access.unavailable.title': 'Správa se nenačetla',

  'access.notice.enrolled': 'Passkey je vytvořený. Přihlaste se jím.',
  'access.notice.ended': 'Relace skončila. Přihlaste se znovu.',
  'access.notice.signedOut': 'Jste odhlášení.',

  'access.problem.cancelled': 'Passkey se nepoužil. Zkuste to znovu.',
  'access.problem.not-allowed':
    'Prohlížeč na této adrese passkey nepovolil. Otevřete správu na veřejné adrese serveru.',
  'access.problem.unsupported': 'Tento prohlížeč nebo zařízení takový passkey nepodporuje.',
  'access.problem.key-type': 'Prohlížeč nepředal veřejný klíč passkey. Zkuste jiný prohlížeč nebo správce hesel.',
  'access.problem.already-enrolled': 'Toto zařízení už passkey pro správu má. Přihlaste se jím.',
  'access.problem.link':
    'Odkaz už byl použitý, vypršel nebo ho nahradil novější. Na serveru spusťte nylzo admin-enroll znovu.',
  'access.problem.refused': 'Server tento passkey nezná. Použijte jiný nebo přidejte nový přes nylzo admin-enroll.',
  'access.problem.busy': 'Server je teď vytížený. Zkuste to za chvíli.',
  'access.problem.offline': 'Server neodpovídá. Zkontrolujte připojení a\u{A0}zkuste to znovu.',
  'access.problem.failed': 'Něco se nepovedlo. Zkuste to znovu.',
};
