# Terms of use

Draft for review by a lawyer. Effective from: [TO FILL IN: publication date]. Version: 0.1.

These terms govern the use of the public Nylzo instance at nylzo.com (the "service"). The Nylzo software is open source under the MIT license; using, changing and distributing the software is governed by that license, not by these terms. Whoever runs their own instance sets their own rules for it.

## 1. Operator

The service is run by Ondřej Misák, [TO FILL IN: postal address; business ID if the service is run as a business] (the "operator"), as an unpaid private project. He runs it mainly for his own use and makes it available to others.

[⚠️ LEGAL REVIEW REQUIRED: Whether an unpaid service run by a private individual is an "information society service" normally provided for remuneration, and therefore whether Regulation (EU) 2022/2065 (DSA) applies. This draft assumes it does and meets the duties of a hosting service under Articles 11, 12, 14, 16 and 17 (analysis in docs/research/G-eu-legal.md, part 4).]

## 2. Accepting these terms

By using the service you accept these terms. If you do not agree with them, do not use the service. The service is free and needs no registration.

[⚠️ LEGAL REVIEW REQUIRED: enforceability of terms accepted by use alone, without an explicit confirmation, and whether a contract with a consumer is formed without registration.]

## 3. How the service works

- Your browser (or the nylzo program) encrypts a message before it sends it. The key is in the part of the link after the # sign, which the browser does not send to the server. The operator does not know the content and cannot recover or hand it over.
- Whoever has the link can open the message, at most as many times as the sender set. If the sender set a passphrase, it is needed too.
- After the last open, cancellation by the sender, deletion by a recipient or expiry (7 days at most), the server deletes the message. A deleted message cannot be recovered.
- The service is for handing something over once, not for storing data. Do not use it as the only place where you keep important information.
- The Transparency page shows the current limits of this instance (attachment size, longest expiry, number of opens, number of messages).

## 4. Rules of use

You may use the service only in line with the law and these terms. In particular, you must not use it to:

- share content that is illegal under the law of the Czech Republic or the European Union, such as child sexual abuse material, terrorist content, or incitement to violence or hatred,
- deceive other people, for example by phishing, fraudulent payment requests or impersonation,
- spread malware or other harmful code,
- infringe the rights of others, for example copyright, trademarks, trade secrets, or the right to privacy and data protection,
- threaten, harass or stalk other people,
- send unsolicited bulk messages,
- get around the limits and the anti-flooding protection of the service, flood it or attack it.

Whoever creates a message is responsible for its content.

## 5. How the operator acts against illegal content

The operator cannot see message content, does not check it and has no technical means to do so. It acts on notices (part 6):

- A person reviews every notice the service stores. It stores at most three notices per message and day, and only about messages that still exist. Nothing is deleted automatically.
- If the operator concludes that a message breaks the law or these terms, it deletes the message. For the next 7 days, a recipient who opens the link sees that the message was deleted, the same as after deletion by a recipient; after that, that it does not exist.
- The service uses automated tools only to protect itself: limits on requests from one address or address range; if set, a daily cap on new messages, from half of which the browser has to complete a short computation before it creates a message; a wait after five wrong passphrases and a lock after 100; and refusals when the server runs out of capacity or disk space. They apply equally to everyone and make no decisions about content.
- The service keeps no contact details of senders or recipients, so the operator cannot send them its decision. You can object to a decision through the contact in part 7.

[⚠️ LEGAL REVIEW REQUIRED: how to meet the statement-of-reasons duty of Art. 17 DSA when the operator has no electronic contact details of the person concerned, and whether the "removed" state shown for the link is enough.]

## 6. Notices of illegal content

Report a message that you believe contains illegal content or breaks these terms through the form on the Report abuse page (nylzo.com/report) or the Report button on the message page, which is available before opening and, after opening, while opens remain. The form sends the server only the message identifier, the reason, the public key derived from the link, a one-time challenge from the server and a signature. Neither the link nor the content leaves the page.

Explain why you consider the content illegal. If you want to know the outcome, also write to the contact in part 7 and give only the message identifier. Do not send the whole link, because it contains the key.

The operator handles notices without undue delay. The service accepts notices only about messages that still exist, keeps at most three notices per message and day, and deletes them 7 days after the day they arrived.

[⚠️ LEGAL REVIEW REQUIRED: whether a form without the notifier's contact meets Art. 16 DSA (confirmation of receipt and informing the notifier), what "without undue delay" means here, and whether the cap of three notices per message and day is acceptable.]

## 7. Point of contact

The point of contact for users and public authorities (Articles 11 and 12 DSA) is: [TO FILL IN: email]. You can write in Czech or English. The contact is also shown on the Transparency page.

## 8. No warranty

The operator built the service with the greatest possible care for security, but provides it as is, without any warranty. In particular, the operator does not guarantee that:

- the service will be available all the time and free of errors,
- a message will be delivered or opened,
- the service will withstand every attack,
- the service will keep running in the future.

The operator may change, limit or end the service at any time, also without prior notice.

Encryption does not protect a message on a compromised device of the sender or the recipient. Whoever has the link first can open the message. A recipient can keep the content. Send the link and the passphrase only to people you trust, each through a different channel.

## 9. Liability

To the widest extent the law allows, the operator is not liable for harm caused by using the service or by not being able to use it, in particular for:

- the loss or deletion of a message,
- the service being unavailable,
- content leaking from a compromised device, through a disclosed link or through what a recipient does,
- use of the service by other people in breach of these terms.

Nothing in these terms excludes or limits liability that the law does not allow to be excluded or limited. This includes liability for harm to a person's natural rights and for harm caused intentionally or by gross negligence (Section 2898 of the Czech Civil Code, Act No. 89/2012 Coll.), and consumer rights that cannot be waived.

[⚠️ LEGAL REVIEW REQUIRED: wording of the liability limitation towards consumers (Sections 1814 and 2898 of the Czech Civil Code), and whether to set an upper limit of compensation for a free service.]

## 10. Intellectual property

The Nylzo software is available under the MIT license at github.com/misaon/nylzo. The license covers the code and the files in the repository. It does not give the right to use the name Nylzo so that another service looks like this service or like a service the operator endorses. Message content belongs to whoever created it. The operator gains no rights to it beyond what is needed to store and hand over the encrypted message.

[⚠️ LEGAL REVIEW REQUIRED: protection of the Nylzo name and logo, which are not a registered trademark and whose files are in the repository under the MIT license.]

## 11. Restricting access

The operator may delete a message that breaks these terms or the law, and may temporarily restrict access to the service from an address range that floods or attacks it, through the automatic limits or by blocking it in the network. You can cancel your message in the browser where you created it or with the manage token from the nylzo program, and you can delete it as a recipient.

## 12. Privacy

The Privacy policy at [TO FILL IN: page address] describes how the service handles personal data.

## 13. Changes to these terms

The operator publishes a new version of these terms with its effective date and announces it on the home page. Messages created before a change remain under the terms in force on the day they were created.

## 14. Governing law and disputes

These terms are governed by the law of the Czech Republic. Czech courts decide disputes. If you are a consumer, you keep the protection the law of the country where you habitually live gives you.

[⚠️ LEGAL REVIEW REQUIRED: choice of law and court towards consumers from other Member States (Art. 6 Rome I, Brussels I recast), and whether an unpaid, non-business operator must inform about out-of-court settlement of consumer disputes (Czech Trade Inspection Authority).]

## 15. Final provisions

If any provision of these terms is invalid, the others remain in force. These terms are issued in Czech and English; if they differ, the Czech version prevails.

[⚠️ LEGAL REVIEW REQUIRED: precedence of the language version towards consumers who do not understand Czech.]
