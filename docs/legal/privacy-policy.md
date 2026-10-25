# Nylzo privacy policy

Effective from [TO FILL IN: date].

## Controller

- The controller is the operator of nylzo.com, a private individual who runs the service free of charge and outside any business. Contact: [TO FILL IN: email].
- The public instance is not offered as a processor under Art. 28 GDPR. An organisation that needs a data processing agreement can run Nylzo itself.

## What we process

- Message content: your browser encrypts it and we cannot read it. Because we do not know what it contains, we treat it as personal data and only store, deliver and delete it. The sender decides what a message contains and who receives it.
- Message details: the sender's settings, the state, the number of opens and wrong passphrases, the size and times to the minute. We store them sealed and can read them only during a request from whoever has the link or the manage key. Only a fingerprint of the identifier and the time we delete the details stay readable. They contain no name or IP address.
- IP address: only in the server's memory for anti-flooding limits, turned into a fingerprint with a random key. It is never written to disk or the log and is gone after a restart. The server reads the User-Agent header in memory to recognise link previews.
- Logs: the time to the minute, the kind of request and the result. No IP address, message identifier or content.
- Abuse reports: the date, the message identifier, the reason without codes from a link, the good-faith confirmation and your email address if you give one. The server seals each report with the operator's key and cannot read it itself.

The Transparency page shows the full, current list.

## Why and on what basis

| Purpose | Legal basis (Art. 6(1) GDPR) |
|---|---|
| Store, deliver and delete a message | point (b) for the sender, otherwise point (f) |
| Protection against flooding and passphrase guessing | point (f), legitimate interest in availability and security; data stays in memory, no profiling |
| Operation and troubleshooting (logs) | point (f) |
| Handling reports | point (c) with Art. 16 DSA where the DSA applies, otherwise point (f) |
| Handling your requests | point (c) |

You can object to processing based on point (f).

## How long

- Content: until the last open, cancellation or deletion, at the latest shortly after expiry (7 days at most).
- Details of an ended message: 7 days, rounded up to the full hour.
- IP fingerprints: in memory, at most until the server restarts.
- Reports, including the email address: 7 days from the day of the report.
- Logs: [TO FILL IN, 7 days at most].

## In your browser

- localStorage: the language you chose, only if you change it.
- sessionStorage: a download token while a message is being opened, until the tab closes.
- IndexedDB: the list of sent messages with a cancel key, only if you turn on "Cancel from this device". The Sent messages page uses it to check the status of your messages.
- This is strictly necessary for features you ask for, so it needs no consent (Section 89(3) of Czech Act No. 127/2005 Coll., Art. 5(3) of Directive 2002/58/EC). We use no cookies or analytics.

## Who receives data

- The hosting provider [TO FILL IN], as a processor. HTTPS is terminated by [TO FILL IN].
- Authorities only where the law requires it. We cannot hand over message content because we do not have the key, and we cannot read message details without the link.
- Data stays in the EU [TO FILL IN, depending on the hosting].

## Your rights

- You have the right to access, rectification, erasure, restriction, portability and to object.
- You can complain to the Czech Office for Personal Data Protection (Pplk. Sochora 27, 170 00 Praha 7, uoou.gov.cz, posta@uoou.gov.cz) or to the authority in the country where you live.
- We keep no accounts or IP addresses and cannot read message details without the link or the manage key, so we usually cannot tell who you are (Art. 11 GDPR). You can see the details of your message yourself on the Sent messages page or with `nylzo status`. Never send the whole link; it contains the key.
- We reply within one month, extendable by two months where justified.

## Other

- We do not check age, and no processing relies on consent.
- We make no automated decisions within the meaning of Art. 22 GDPR.
- We publish a new version of this policy with its effective date.
