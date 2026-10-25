# Privacy policy

Draft for review by a lawyer. Effective from: [TO FILL IN: publication date]. Version: 0.1.

This policy describes how the public Nylzo instance at nylzo.com (the "service") handles personal data. Nylzo is open-source software under the MIT license, and anyone can run it on their own server. This policy does not cover other instances; whoever runs them is responsible for them.

## 1. Controller

The controller is Ondřej Misák, [TO FILL IN: postal address; business ID if the service is run as a business] (the "operator"). Contact for personal data questions: [TO FILL IN: email]. The operator has not appointed a data protection officer.

[⚠️ LEGAL REVIEW REQUIRED: Whether a private individual who runs the service for free and mainly for personal use falls under the purely personal activity exemption (Art. 2(2)(c) GDPR). Because the service is open to the public, this draft assumes the exemption does not apply.]

## 2. Summary

- The service has no accounts and does not ask for your name, email or phone number.
- Your browser encrypts a message before it sends it. The key is in the part of the link after the # sign, which the browser does not send to the server, so the operator cannot read the message or the name of an attachment.
- The server does not store IP addresses on disk or write them to its log. It uses them only in memory to protect the service from flooding and forgets them on restart.
- The site uses no cookies, analytics, advertising or third-party scripts.
- A message is deleted after its last open, when it is cancelled or deleted by a recipient, or shortly after it expires (7 days at most). A short record of how it ended stays for 7 days.

## 3. What data the service processes

### 3.1 Message content

Your browser encrypts the text and the attachment of a message with AES-256-GCM. The server receives and stores only the encrypted data. The key is in the link after the # sign; the browser does not send that part of the link to the server, and the operator does not have it. The operator therefore cannot read the content or the name of an attachment, and cannot see whether a message contains personal data.

If you put other people's personal data in a message, you are responsible for being allowed to share it.

[⚠️ LEGAL REVIEW REQUIRED: Whether encrypted content the operator cannot decrypt is personal data from the operator's point of view (CJEU C-413/23 P, EDPS v SRB, 4 Sept 2025), and the operator's role for the content (controller, or processor on behalf of the sender). See docs/research/G-eu-legal.md, part 1.]

### 3.2 Message details

To hand out a message and delete it, the server stores for each message:

- the random identifier from the link,
- whether a passphrase protects it, the format version and the sender's settings (number of opens, expiry),
- its state (waiting, opened, or how it ended),
- how many opens remain, how many times someone entered a wrong passphrase and, after wrong passphrases, when the next attempt is allowed,
- the size rounded up to whole blocks (4 KiB for a text-only message, 64 KiB for a message with an attachment),
- the times of creation, expiry and first opening, rounded to the minute,
- fingerprints of public keys (HMAC with a server secret) that the server uses to check signed requests,
- while a message is being opened, a record of the download in progress (a random identifier, its fingerprint, its end time and the number of downloads).

None of these contains an IP address, a name or anything else about who sent or opened a message.

### 3.3 IP address

Every request to the server carries the IP address of your connection. The server does not store it in the database or in any file and does not write it to the log. In memory, it derives short fingerprints of the address and of its wider range (HMAC with a random key that is created each time the server starts and never stored) and uses them to count requests per second, per hour and per day. The fingerprints stay in memory until the table that holds them fills up and drops old entries, or until the server restarts.

To recognise link previews from chat apps, the server also reads the User-Agent header of a request in memory. It does not store or log it.

The hosting provider and whoever terminates the encrypted HTTPS connection also see the IP address (see part 6).

### 3.4 Logs

The server logs the time truncated to the minute, the method and kind of path of a request, the result and how long it took. For creating, opening and downloading a message it leaves out the duration, because it would reveal the size of the message. It never logs an IP address, a message identifier, keys, signatures, content or the size of a message.

### 3.5 Abuse reports

When you report a message as abusive, the server stores the date, the message identifier and the reason you give, but only if the message still exists. Before sending, the page replaces every # sign with the text after it up to the next space, and every run of 22 or more letters, digits, hyphens or underscores, with "[redacted]", so the key from a link does not end up in the report; the server does the same again before storing. The server does not find out who sent a report and stores no IP address or contact with it.

### 3.6 Counters without addresses

To protect the service, the server stores the number of reports per message and day and, for a short time, used one-time challenges and anti-flooding tokens. If the operator has set a daily cap on new messages, it also stores daily totals of created messages. None of these contains an IP address.

### 3.7 Data kept only in your browser

The site keeps some data in your browser's storage. It does not send this data to the server, except as described below.

| Storage | What it holds | When and for how long |
|---|---|---|
| localStorage | the language you chose | only if you change the language; until you clear the browser data |
| sessionStorage | while a message is being opened, its identifier and the download token the server issued; a marker that deriving the key from a passphrase did not finish | until you close the tab |
| IndexedDB | the list of sent messages: identifier, times and a cancel key that the browser does not allow to be exported | by default, unless you turn off "Cancel from this device"; an entry is removed at your next visit after 7 days past the message's expiry (or its end, if the Sent messages page has seen it), or when you forget it |

The browser sends the download token back to the server to finish or resume a download. When you open the Sent messages page, it asks the server for the status of each remembered message, with the identifier, the public part of the cancel key and a signature; cancelling a message sends the same. If you saved a language, the home page sends you to the English or Czech address, so the server sees which one you asked for.

[⚠️ LEGAL REVIEW REQUIRED: Whether this storage falls under the strictly necessary exemption of Section 89(3) of Czech Act No. 127/2005 Coll. and Art. 5(3) of Directive 2002/58/EC, so that no consent is needed (analysis in docs/research/G-eu-legal.md, part 7).]

### 3.8 What the service does not process

The service has no user accounts and does not process names, email addresses or phone numbers. It uses no cookies, analytics tools, advertising, or scripts, fonts or other files from third parties. It does not sell data or pass it on for marketing.

## 4. Purposes and legal bases

| Purpose | Data | Legal basis |
|---|---|---|
| Store a message and hand it to the holder of the link | encrypted content, message details | performance of a contract, namely the terms of use (Art. 6(1)(b) GDPR) |
| Protect the service from flooding and abuse | IP address in memory, counters without addresses | the operator's legitimate interest in a secure and available service (Art. 6(1)(f) GDPR) |
| Operation and troubleshooting | logs | the operator's legitimate interest (Art. 6(1)(f) GDPR) |
| Handling notices of illegal content | abuse reports | legal obligation under Art. 16 of Regulation (EU) 2022/2065 (DSA) (Art. 6(1)(c) GDPR) |
| Site features in your browser | data in part 3.7 | strictly necessary storage you asked for |

[⚠️ LEGAL REVIEW REQUIRED: The choice of legal bases, in particular whether free use without registration forms a contract (point b) or legitimate interest (point f) fits better, and the balancing test for the legitimate interest.]

## 5. How long data is kept

| Data | Retention |
|---|---|
| Encrypted content | until the last open, cancellation, deletion by a recipient, removal after a report or expiry (7 days at most); deleted on the next request for the message or by the regular cleanup, which runs every 5 minutes by default; after a last open that the browser did not confirm, when the download time ends (at least 2 minutes) |
| Message details | until the message ends; then only a record of how and when it ended, when it was first opened and the fingerprints of its keys, for 7 days |
| Record of a download in progress | until the download is confirmed, or until the first cleanup after its time runs out (at least 2 minutes, longer for large messages) |
| IP address and derived fingerprints in memory | until the in-memory table drops them, at most until the server restarts |
| Logs | [TO FILL IN: depends on the hosting setup; 7 days at most is recommended. The Docker Compose example rotates logs by size only (3 files of 10 MB) and sets no time limit] |
| Abuse reports | up to 7 days from the day of the report |
| Daily counters | 24 hours after the day ends |
| One-time challenges and anti-flooding tokens | a few minutes |
| Data in your browser | see the table in part 3.7 |

The server overwrites deleted database records with zeros. It deletes the files holding encrypted content; what remains of them on disk stays encrypted, and nobody can read it without the key.

## 6. Who receives data

- Hosting provider: [TO FILL IN: name, registered office, country of the servers]. It runs the physical server and, as a processor, sees IP addresses, request times and sizes and the encrypted data. It does not see message content. [⚠️ LEGAL REVIEW REQUIRED: data processing agreement with the hosting provider under Art. 28 GDPR.]
- The encrypted HTTPS connection is terminated by [TO FILL IN: who; in the recommended Docker Compose setup this is a Caddy proxy on the same server, controlled by the operator, which logs no requests and no error details]. Whoever terminates the connection sees IP addresses and requests, not message content.
- Public authorities: only where the law requires it, and only data the service has. The operator cannot hand over message content because it does not have the key.

The operator does not pass data to anyone else.

## 7. Transfers outside the EU

The operator does not intend to transfer personal data outside the European Economic Area. The servers of the service are in [TO FILL IN: EU country]. [⚠️ LEGAL REVIEW REQUIRED: complete once the hosting is chosen; for a provider based outside the EU, assess transfers and access under third-country law (docs/research/G-eu-legal.md, parts 2 and 3).]

## 8. Your rights

Under the GDPR you have the right to access your data, have it corrected or erased, restrict processing, data portability, and the right to object to processing based on legitimate interest. You also have the right to lodge a complaint with the Czech Office for Personal Data Protection (Úřad pro ochranu osobních údajů), Pplk. Sochora 27, 170 00 Praha 7, www.uoou.cz, or with the supervisory authority where you live.

The service has no accounts and does not store IP addresses, so the operator usually cannot tell which data belongs to you (Art. 11 GDPR). You can cancel your message on the Sent messages page in the browser where you created it (unless you turned off "Cancel from this device"), or with the manage token from the nylzo command-line program. As a recipient you can delete it. If you contact the operator about a specific message, give only its identifier (the part of the link before the # sign). Never send the whole link to anyone, because it contains the key.

The operator answers requests without undue delay and within one month at the latest.

[⚠️ LEGAL REVIEW REQUIRED: wording of the Art. 11 and Art. 12 GDPR part for a service without accounts.]

## 9. Security

- Encryption happens in your browser or in the nylzo command-line program; the server never receives the key.
- Instead of keys, the server stores only their fingerprints with a secret kept outside the database.
- A copy of the database, files and logs reveals neither message content nor IP addresses; an automated test checks this with every code change.
- The code, the protocol specification and the description of what the server stores are public. The Transparency page shows the details and limits of this instance.

No system is perfectly secure. Encryption does not protect a message on a compromised device of the sender or the recipient, and it does not stop a recipient from keeping the content.

## 10. Children

The service does not check age. It is not intended for children under 15 without the supervision of a parent or guardian. [⚠️ LEGAL REVIEW REQUIRED: whether an age limit is needed when processing does not rely on consent (Art. 8 GDPR, Section 7 of Czech Act No. 110/2019 Coll.).]

## 11. Automated decision-making

The service makes no automated decisions about people within the meaning of Art. 22 GDPR. Its technical safeguards apply equally to everyone: limits on requests from one address or address range, a wait after five wrong passphrases and a lock after 100, a cap on stored reports, and refusals when the server runs out of capacity or disk space. They may temporarily refuse a request.

## 12. Changes to this policy

The operator publishes a new version of this policy on this page with its effective date. A material change is announced on the home page of the service.

## 13. Contact

Send questions and requests about personal data to [TO FILL IN: email].
