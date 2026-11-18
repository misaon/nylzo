# Getting started

This page is for people who send or open a Nylzo message in a browser.

## Send a message

1. Open [nylzo.com](https://nylzo.com) or another Nylzo instance.
2. Type the message, attach a file, or both.
3. Choose when the link expires and how many times it can be opened.
4. Optionally turn on Protect with a passphrase and keep the random one Nylzo suggests, or type your own.
5. Choose Create link. Send the link to the recipient and the passphrase another way.

Your browser encrypts the message before uploading it. The key is the part of the link after `#`, which browsers never send to the server. Track in Sent, offered once the link exists, shows whether it was opened and lets you cancel it.

## Limits

nylzo.com uses the defaults below. Another instance lists its own on its `/transparency` page.

- Text: up to 100 KiB.
- Attachment: one file, up to 5 MiB.
- Expiry: 1 hour, 1 day, 3 days or 7 days; 1 day unless you choose.
- Opens: 1 unless you choose, up to 10.
- Passphrase: at least 15 characters, not repetitive, sequential or a common password.

## Open a message

The link opens a page with a Show message button. Loading it spends no open, not even for a chat app's link preview.

- Enter the passphrase if the message has one. After five wrong attempts each further try waits longer, and after 100 the message stays locked until it expires.
- The page shows the message for 15 minutes, then removes it. Save what you need.
- An attachment downloads only after you confirm a warning that shows its real extension. Nylzo cannot scan encrypted files, so open one only if you trust the sender.
- Delete without opening ends the message instead. With a passphrase set, this needs the passphrase.

A message ends at its last open, at expiry, or when the sender cancels it or a reader deletes it.

## Keep links private

Anyone with the link can open the message. Keep real links and passphrases out of public issues, screenshots, support chats and AI assistants. A clicked link stays in the reader's browser history while opens remain; a passphrase protects against that. A recipient can always keep a copy of what they read.

See also the [CLI guide](CLI.md) and the [security model](Security-model.md).
