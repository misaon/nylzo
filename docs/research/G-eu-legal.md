# G: What "the public instance must be hosted in the EU" means in practice

Research date: 2026-10-21. Scope: a small open-source service for anonymous one-time encrypted messages and files (up to 5 MB), no accounts, operator is a Czech individual or company. This is a source collection, not legal advice.

Method note: every claim below was checked by opening the page. EUR-Lex (eur-lex.europa.eu) blocked all automated fetches during this session (AWS WAF challenge on `/eli/...`, `/legal-content/.../HTML`, `/legal-content/.../PDF`). Where EUR-Lex was the natural source, the text was verified against (a) legislation.gov.uk "as adopted by EU" mirrors for the GDPR and the ePrivacy Directive, and (b) a third-party reproduction of the DSA (eu-digital-services-act.com, Cyber Risk GmbH). Those items are marked PARTIAL because the Official Journal page itself was not opened. CJEU press releases were opened as PDFs and text-extracted locally.

Status key: VERIFIED = opened the page and the quote is on it. PARTIAL = opened a page but it is a mirror, a secondary source, or it supports only part of the claim. NOT FOUND = looked and could not confirm.

---

## 1. Personal data scope

- Claim: A dynamic IP address is personal data for a website operator when the operator has legal means to identify the visitor via the ISP (CJEU Breyer, C-582/14, 19 Oct 2016).
  URL: https://curia.europa.eu/jcms/upload/docs/application/pdf/2016-10/cp160112en.pdf (Press Release No 112/16)
  Quote: "constitutes personal data, with respect to the operator of the website, if that operator has the legal means allowing it to identify the visitor"
  Status: VERIFIED

- Claim: Breyer also recognises that a site operator may have a legitimate interest in storing visitor IPs to defend against attacks, beyond the single session.
  URL: same press release
  Quote: "may have a legitimate interest in storing certain personal data relating to visitors to that website in order to protect itself against cyberattacks"
  Status: VERIFIED

- Claim: GDPR Art 4(1) defines personal data by reference to an identified or identifiable person; Art 4(5) defines pseudonymisation.
  URL: https://www.legislation.gov.uk/eur/2016/679/article/4/adopted
  Quote: "'personal data' means any information relating to an identified or identifiable natural person ('data subject')"
  Status: PARTIAL (official UK mirror of the EU text as adopted; EUR-Lex blocked)

- Claim: CJEU in EDPS v SRB (C-413/23 P, 4 Sept 2025) held that pseudonymised data is not personal data in all cases for every person; identifiability is judged from the perspective of the party processing it, in the circumstances.
  URL: https://curia.europa.eu/jcms/upload/docs/application/pdf/2025-09/cp250107en.pdf (Press Release No 107/25)
  Quote: "pseudonymised data must not be regarded as constituting, in all cases and for every person, personal data"
  Status: VERIFIED

- Claim: In the same judgment, the controller's transparency duties are assessed at collection, from the controller's point of view, regardless of whether the recipient can identify anyone.
  URL: same press release
  Quote: "the identifiable nature of the data subject must be assessed at the time of collection of the data and from the point of view of the controller"
  Status: VERIFIED

- Claim: EDPB Guidelines 01/2025 on pseudonymisation state the EDPB's own view on whether pseudonymised data stays personal data.
  URL tried: https://www.edpb.europa.eu/our-work-tools/documents/public-consultations/2025/guidelines-012025-pseudonymisation_en (consultation page, no text) and .../our-documents/guidelines/guidelines-012025-pseudonymisation_en (404)
  Status: NOT FOUND (page did not expose the text; PDF not opened)

Reasoning (flag for legal review): For this service the operator holds (a) ciphertext it cannot decrypt, (b) a link id, (c) web server/CDN logs with IP addresses and timestamps. After SRB, (a) alone is arguably not personal data from the operator's perspective if the operator has no key and no reasonable means to get one. But (c) is personal data under Breyer, and (a)+(b)+(c) together are linkable (who uploaded what, when). The practical conclusion is that the operator is a GDPR controller for logs and metadata whatever the status of the ciphertext, so GDPR Chapter V (transfers) attaches to wherever logs and metadata go, not only to the blob store.

---

## 2. International transfers (status as of Oct 2026)

- Claim: The General Court dismissed Latombe's annulment action against the EU-US DPF adequacy decision on 3 Sept 2025 (T-553/23).
  URL: https://curia.europa.eu/jcms/upload/docs/application/pdf/2025-09/cp250106en.pdf (Press Release No 106/25)
  Quote: "The General Court dismisses the action for annulment."
  Status: VERIFIED

- Claim: The General Court found the US ensured adequate protection at the date of the decision; DPRC independence safeguards were sufficient; bulk collection needs ex post, not prior, judicial review.
  URL: same press release
  Quote: "there is nothing in Schrems II to suggest that that collection must necessarily be subject to prior authorisation issued by an independent authority"
  Status: VERIFIED

- Claim: Latombe appealed to the Court of Justice; case number C-703/25 P, lodged 31 Oct 2025; appeal pending.
  URL: https://digitalpolicyalert.org/event/35459-latombe-filed-appeal-against-general-court-dismissal-of-challenge-to-european-unionunited-states-data-protection-framework-adequacy-decision-in-latombe-v-commission
  Quote: "On 31 October 2025, Philippe Latombe filed an appeal (Case C-703/25 P) against a ruling by the General Court"
  Status: PARTIAL (secondary tracker; the OJ notice C/2025/6610 on EUR-Lex and the Curia docket could not be opened; WilmerHale blog confirms the 31 Oct 2025 filing but gives no number)

- Claim: An appeal to the Court of Justice does not, in principle, suspend the judgment appealed, so the DPF adequacy decision remains in force while C-703/25 P is pending.
  URL: https://curia.europa.eu/jcms/upload/docs/application/pdf/2025-09/cp250107en.pdf (standard note in Curia press releases)
  Quote: "In principle, the appeal does not have suspensive effect."
  Status: VERIFIED (general procedural statement; no case-specific suspension order was found)

- Claim: Standard Contractual Clauses (Commission Implementing Decision (EU) 2021/914, 4 June 2021) remain the main alternative transfer tool.
  URL: https://commission.europa.eu/law/law-topic/data-protection/international-dimension-data-protection/standard-contractual-clauses-scc_en
  Quote: "On 4 June 2021, the Commission issued modernised standard contractual clauses"
  Status: PARTIAL (the decision number 2021/914 appears on the page only in the EUR-Lex link; the EUR-Lex decision itself was blocked). Vendor usage confirmed: Vercel's DPA cites "the 2021 Standard Contractual Clauses approved by the European Commission in decision 2021/914" (https://vercel.com/legal/dpa, VERIFIED).

- Claim: EDPB and EDPS (joint response to LIBE, 10 July 2019) take the position that a US CLOUD Act request is not by itself a lawful basis for an EU provider to disclose or transfer personal data; Art 48 GDPR requires an international agreement such as an MLAT.
  URL: https://www.edpb.europa.eu/system/files/documents/files/file1/edpb_edps_joint_response_us_cloudact_coverletter.pdf and https://www.edpb.europa.eu/system/files/documents/files/file2/edpb_edps_joint_response_us_cloudact_annex.pdf
  Quote (annex): "A request from a foreign authority does not in itself constitute a legal ground for transfer."
  Quote (annex): "service providers subject to EU law cannot legally base the disclosure and transfer of personal data to the US on such requests"
  Status: VERIFIED (letter dated Brussels, 10 July 2019, ref OUT2019-0007; the EDPB web page lists 12 July 2019 as publication date)

- Claim: The same letter frames the exposure as a jurisdiction problem, not a storage-location problem: data held by operators under US jurisdiction is reachable regardless of where it sits.
  URL: cover letter above
  Quote: "massive amounts of electronic communication data are processed and stored by operators falling under US jurisdiction"
  Status: VERIFIED

- Claim: GDPR Art 48 text.
  URL: https://www.legislation.gov.uk/eur/2016/679/article/48/adopted (opening) and EDPB annex p. 3 (quoting the operative words)
  Quote: "may only be recognised or enforceable in any manner if based on an international agreement, such as a MLAT"
  Status: PARTIAL (mirror plus EDPB quotation; EUR-Lex blocked)

- Claim: Cloudflare's own DPA says processing may happen outside the EEA even for EU customers, with DPF as the primary mechanism and SCCs as fallback.
  URL: https://www.cloudflare.com/cloudflare-customer-dpa/ (Version 6.4, effective 3 April 2026)
  Quote: "the parties anticipate that Cloudflare (and its sub-Processors) may process outside of the European Economic Area"
  Status: VERIFIED

Verified facts vs. reasoning, separated:

Facts: (1) DPF adequacy stands today; (2) an appeal is pending; (3) SCCs are available; (4) EDPB/EDPS say a CLOUD Act demand alone is not a GDPR transfer basis and Art 48 points to MLAT; (5) US-owned vendors' DPAs reserve the right to process outside the EEA and name a US entity as the contracting party (Cloudflare, Inc.; Vercel Inc.; Better Stack, Inc.).

Reasoning (flag for legal review): "Hosted in the EU" has two readings. Narrow: data at rest and compute physically in an EU data centre. A US-owned provider's EU region meets that. Broad: data under EU jurisdiction only. A US-owned provider's EU region does not meet that, because the CLOUD Act reaches the provider as a US legal person and the EDPB's answer to that is "refer them to the MLAT", which the operator cannot enforce against the vendor. Residual risk with a US-owned EU region: (a) if the Court of Justice invalidates the DPF, any out-of-EEA processing the vendor reserves in its DPA falls back on SCCs plus a transfer impact assessment the operator must document; (b) vendor support, logs and control planes often sit in the US (see Sentry and Cloudflare Workers Logs below) even when the "region" is EU; (c) a CLOUD Act demand to the vendor is outside the operator's control. For this service the content is ciphertext, so the realistic exposure is metadata and IP logs. An EU-domiciled provider (Hetzner, Scaleway) removes (a) and (c) for infrastructure; it does not remove them for US-based SaaS add-ons.

---

## 3. Vendor statements

### Cloudflare (domicile: Cloudflare, Inc., San Francisco, USA)

- Claim: Contracting entity and transfer mechanism.
  URL: https://www.cloudflare.com/cloudflare-customer-dpa/
  Quote: "Cloudflare complies with the Data Privacy Framework"
  Status: VERIFIED (entity given as Cloudflare, Inc., 101 Townsend Street, San Francisco; DPA v6.4 effective 3 April 2026)

- Claim: The Data Localization Suite (Regional Services, Customer Metadata Boundary, Geo Key Manager) is an Enterprise add-on.
  URL: https://developers.cloudflare.com/data-localization/ and https://developers.cloudflare.com/data-localization/regional-services/
  Quote: "Enterprise-only paid add-on" / "Regional Services is an Enterprise add-on."
  Status: VERIFIED

- Claim: Regional Services restricts where Workers execute, but Worker code and secrets are replicated globally; subrequests and cron/queue triggers are not regionalised.
  URL: https://developers.cloudflare.com/data-localization/how-to/workers/
  Quote: "Workers code and secrets are deployed globally to all Cloudflare data centers."
  Status: VERIFIED

- Claim: Customer Metadata Boundary keeps customer logs in the EU or US region only.
  URL: https://developers.cloudflare.com/data-localization/metadata-boundary/
  Quote: "ensures that Customer Logs stay in the region you select" / "Customer Metadata Boundary supports these two regions only"
  Status: VERIFIED

- Claim: Workers Logs state no storage region; retention is 3 days (Free) or 7 days (Paid).
  URL: https://developers.cloudflare.com/workers/observability/logs/workers-logs/
  Quote: "Data is written to your Cloudflare Account"
  Status: VERIFIED (no region statement on the page)

- Claim: R2 supports an `eu` jurisdiction that guarantees storage location; KV and D1 do not support jurisdictional restrictions.
  URL: https://developers.cloudflare.com/r2/reference/data-location/ and https://developers.cloudflare.com/data-localization/compatibility/
  Quote: "Jurisdictional Restrictions guarantee objects in a bucket are stored within a specific jurisdiction." / "Jurisdictional Restrictions (storage) for Workers KV pairs is not supported today."
  Status: VERIFIED

- Claim: Cloudflare commits to contest government requests and states it has never handed over keys.
  URL: DPA above, clause 8
  Quote: "Cloudflare has never turned over our encryption or authentication keys"
  Status: VERIFIED

Support access from outside the EU: NOT FOUND on the pages opened.

### Oracle Cloud Infrastructure (domicile: Oracle Corporation, USA; not verified from a page in this session)

- Claim: Customer content is stored in the ordered data centre region and replicated only within it.
  URL: https://www.oracle.com/a/ocom/docs/ocloud-hosting-delivery-policies-3089853.pdf (file states Version 3.3, Effective Date June 2022)
  Quote: "Your Content will be stored in the data center region applicable to such services."
  Quote: "Oracle may replicate Your Content to other locations within the applicable data center region in support of data durability."
  Status: VERIFIED (check whether a newer version supersedes this file)

- Claim: OCI commercial EU regions include Frankfurt, Amsterdam, Paris, Marseille, Madrid, Milan, Turin, Stockholm (Zurich is Switzerland, not EU).
  URL: https://docs.oracle.com/en-us/iaas/Content/General/Concepts/regions.htm
  Quote: "Regions are independent of other regions and can be separated by vast distances—across countries or even continents."
  Status: VERIFIED

- Claim: Oracle states OCI does not move content out of the selected region; EU Sovereign Cloud restricts operations and support to EU residents.
  URL: https://www.theregister.com/2022/07/13/oracle_eu_sovereign_cloud/ (13 July 2022; Oracle's own pages returned 403)
  Quote: "OCI already does not move customer content from the regions our customers select for their workloads."
  Status: PARTIAL (secondary press quote; Oracle sovereign-cloud page and blog not opened)

Logs/backups and support-from-outside-EU for standard regions: NOT FOUND.

### Scaleway (domicile: Scaleway SAS, Paris, France)

- Claim: Legal domicile.
  URL: https://www.scaleway.com/en/legal-notice/
  Quote: "8, rue de la Ville-l'Évêque, 75008 Paris, France" / "SIREN: 433 115 904 RCS Paris"
  Status: VERIFIED

- Claim: Scaleway states data and processing stay under European jurisdiction and claims to be outside the CLOUD Act.
  URL: https://www.scaleway.com/en/security-and-compliance/
  Quote: "Scaleway guarantees that your data and data processing remain entirely within European jurisdiction."
  Quote: "Free from the US CLOUD Act, your data remains resilient, secure, and strictly under your control."
  Status: VERIFIED (vendor marketing claim; no independent legal confirmation found)

- Claim: SecNumCloud is in progress, not obtained.
  URL: same page; also https://www.scaleway.com/en/blog/secnumcloud-qualification-trusted-cloud/
  Quote: "Currently undergoing qualification, find out more about our position and the milestones already achieved by our teams."
  Status: VERIFIED

- Claim: Regions are Paris, Amsterdam, Warsaw and Milan, each with 3 AZs.
  URL: https://www.scaleway.com/en/docs/account/reference-content/products-availability/
  Quote: table columns "Paris", "Amsterdam", "Warsaw", "Milan", each "3 AZ"
  Status: VERIFIED

- Claim: Sub-processors are EU-located colocation providers (some US-headquartered); one Canadian sub-processor for Dedibox VPS.
  URL: https://www.scaleway.com/en/subprocessorlist/ ("Last reviewed: July 2025")
  Quote: "by using Scaleway's services and agreeing to our DPA, Customer gives Scaleway general authorization to add, modify or remove sub-processors"
  Status: VERIFIED

Logs/backups statement and support-from-outside-EU statement: NOT FOUND (the DPA PDF link on https://www.scaleway.com/en/contracts/ was not exposed to the fetcher; https://www.scaleway.com/en/gdpr/ and /en/sovereignty/ returned 404).

### Hetzner (domicile: Hetzner Online GmbH, Gunzenhausen, Germany)

- Claim: Legal domicile.
  URL: https://www.hetzner.com/legal/legal-notice/
  Quote: "Industriestr. 25, 91710 Gunzenhausen, Germany" / "Ansbach Registration Office, HRB 6089"
  Status: VERIFIED

- Claim: Locations are Falkenstein, Nuremberg, Helsinki (own parks) plus Ashburn, Hillsboro, Singapore (colocation).
  URL: https://docs.hetzner.com/cloud/general/locations/ and https://www.hetzner.com/unternehmen/rechenzentrum/
  Quote: "We do not have our own data center parks in the USA or in Singapore."
  Status: VERIFIED

- Claim: Data at rest, backups and support access stay in the EU when an EU location is chosen.
  URL: https://docs.hetzner.com/general/company-and-policy/data-protection-at-hetzner/
  Quote: "If you have chosen a product location within the EU, the backups also remain within the EU."
  Quote: "Support access is generally provided by our teams within the EU."
  Status: VERIFIED

- Claim: Hetzner's DPA restricts processing to the EU/EEA and requires client consent for any third-country transfer.
  URL: https://www.hetzner.com/AV/DPA_en.pdf (DPA version 1.1, 10 Feb 2025)
  Quote: "will occur exclusively in a member state of the European Union or in another member state party to the Agreement on the European Economic Area"
  Quote: "Any transfer to a third country will require the prior consent of the Client"
  Status: VERIFIED

### Vercel (added because it was named in the brief)

- Claim: Vercel Inc. is a Delaware corporation whose primary processing is in the US; it uses SCCs; functions default to Washington DC and can be pinned to an EU region, but the Hobby plan allows one region only.
  URL: https://vercel.com/legal/dpa and https://vercel.com/docs/functions/configuring-functions/region
  Quote: "Customer acknowledges that, as of the Effective Date, Vercel's primary processing facilities are in the United States."
  Status: VERIFIED

---

## 4. Hosting liability under the DSA (Regulation (EU) 2022/2065)

All DSA quotes below come from https://www.eu-digital-services-act.com/ (third-party reproduction). EUR-Lex was blocked. Status for each is PARTIAL for that reason; the wording matched the Official Journal in every spot-check against my recollection, but the OJ page itself was not opened.

- Claim: Definition of "hosting" (Art 3(g)(iii)).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_3.html
  Quote: "a 'hosting' service, consisting of the storage of information provided by, and at the request of, a recipient of the service;"
  Status: PARTIAL

- Claim: "Online platform" requires dissemination to the public (Art 3(i)).
  URL: same
  Quote: "a hosting service that, at the request of a recipient of the service, stores and disseminates information to the public,"
  Status: PARTIAL

- Claim: Art 6(1) liability exemption for hosting on the no-knowledge / act-expeditiously conditions.
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_6.html
  Quote: "upon obtaining such knowledge or awareness, acts expeditiously to remove or to disable access to the illegal content."
  Status: PARTIAL

- Claim: Art 7 protects voluntary own-initiative investigations from costing the exemption.
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_7.html
  Quote: "shall not be deemed ineligible for the exemptions from liability referred to in Articles 4, 5 and 6 solely because they, in good faith"
  Status: PARTIAL

- Claim: Art 8 forbids general monitoring obligations.
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_8.html
  Quote: "No general obligation to monitor the information which providers of intermediary services transmit or store"
  Status: PARTIAL

- Claim: Art 11(1) single point of contact for authorities (all intermediaries, no size exemption).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_11.html
  Quote: "shall designate a single point of contact to enable them to communicate directly, by electronic means, with Member States' authorities"
  Status: PARTIAL

- Claim: Art 12(1) single point of contact for recipients (all intermediaries).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_12.html
  Quote: "Providers of intermediary services shall designate a single point of contact."
  Status: PARTIAL

- Claim: Art 14(1) terms and conditions must state content restrictions (all intermediaries).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_14.html
  Quote: "Providers of intermediary services shall include information on any restrictions that they impose"
  Status: PARTIAL

- Claim: Art 15(2) exempts micro and small enterprises from transparency reporting.
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_15.html
  Quote: "Paragraph 1 of this Article shall not apply to providers of intermediary services that qualify as micro or small enterprises"
  Status: PARTIAL

- Claim: Art 16(1) notice-and-action mechanism (all hosting services, no size exemption).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_16.html
  Quote: "Providers of hosting services shall put mechanisms in place to allow any individual or entity to notify them"
  Status: PARTIAL

- Claim: Art 17(1) statement of reasons (all hosting services).
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_17.html
  Quote: "Providers of hosting services shall provide a clear and specific statement of reasons to any affected recipients"
  Status: PARTIAL

- Claim: Art 19(1) micro/small exemption applies to the online-platform Section only, not to plain hosting.
  URL: https://www.eu-digital-services-act.com/Digital_Services_Act_Article_19.html
  Quote: "This Section, with the exception of Article 24(3) thereof, shall not apply to providers of online platforms"
  Status: PARTIAL (confirms the brief's reading: Art 19 is scoped to online platforms)

- Claim: Czechia's Digital Services Coordinator is the Czech Telecommunication Office (ČTÚ); the Commission referred Czechia to the Court of Justice on 7 May 2025 for not empowering it.
  URL: https://digital-strategy.ec.europa.eu/en/policies/dsa-dscs
  Quote: "In further steps, the Commission referred Czechia, Spain, Poland, and Portugal to the Court of Justice of the European Union."
  Status: VERIFIED

- Claim: ČTÚ was designated by government resolution No 590 of 16 Aug 2023; without the adaptation law it cannot inspect or fine.
  URL: https://ctu.gov.cz/sites/default/files/obsah/soubory-ke-stazeni/Let%C3%A1k_DSA.pdf
  Quote: "ČTÚ byl usnesením vlády č. 590 ze dne 16. 8. 2023 odsouhlasen na pozici koordinátora digitálních služeb v ČR."
  Quote (listed under what ČTÚ cannot do without the law): "Ukládat sankce za porušení DSA"
  Status: VERIFIED

- Claim: The Czech adaptation law (zákon o digitální ekonomice, sněmovní tisk 69) is still in the Chamber of Deputies as of 21 Oct 2026: government bill 12 Dec 2025, first reading 10 Mar 2026, second reading 1 Jul 2026, committee opinion 4 Sep 2026, no third reading recorded.
  URL: https://www.psp.cz/sqw/historie.sqw?o=10&t=69
  Quote: "Stav projednávání ke dni: 21. října 2026"
  Status: VERIFIED

- Claim: Czech press confirms the DSA applies in Czechia but ČTÚ lacks enforcement powers without the law.
  URL: https://www.lupa.cz/aktuality/evropska-komise-zaluje-cesko-kvuli-neprijeti-pravomoci-pro-digitalniho-regulatora/ (8 May 2025)
  Quote: "Bez zákona sice nařízení v Česku platí, ale regulátor nemá pravomoci je vynucovat či udělat sankce za jeho neplnění."
  Status: VERIFIED (commentary)

Reasoning (flag for legal review): The service stores information provided by and at the request of a recipient (the uploader), so it fits Art 3(g)(iii) "hosting" on its face. It is not an "online platform" because a one-time link shared by the uploader is not dissemination "to the public" (Art 3(i)); this is the key classification question a lawyer should confirm, since it decides whether Section 3 (Arts 19-28) applies. If it is plain hosting, the obligations that apply regardless of size are Arts 11, 12, 14, 16 and 17 (plus Art 13 if the operator were outside the EU, which it is not). Art 15 reporting is waived for micro/small. There is no size waiver for Arts 11-14, 16, 17. Enforcement in Czechia is ČTÚ, but it cannot sanction until the adaptation law passes; the Regulation itself already binds the operator.

---

## 5. Logging and retention duties

- Claim: The Czech data retention duty (6 months) in zákon č. 127/2005 Sb., § 97(3), applies to entities running a public communications network or providing a publicly available electronic communications service.
  URL: https://www.zakonyprolidi.cz/cs/2005-127
  Quote: "je povinna uchovávat po dobu 6 měsíců provozní a lokalizační údaje, které jsou vytvářeny nebo zpracovávány při zajišťování jejích veřejných komunikačních sítí"
  Status: VERIFIED

- Claim: The same Act's definition of "electronic communications service" (§ 2(3)(a)) excludes services providing content carried over networks.
  URL: same
  Quote: "s výjimkou služeb poskytujících obsah přenášený prostřednictvím sítí a služeb elektronických komunikací nebo vykonávajících redakční dohled nad tímto obsahem"
  Status: VERIFIED

- Claim: There is an EU or Czech law requiring a small web hosting service (not an ECS provider) to retain access logs for a fixed period.
  Status: NOT FOUND. Searched for ÚOOÚ guidance on web server logs and for any hosting-specific retention duty; found only commentary (epravo.cz, lupa.cz) stating logs are kept as long as their purpose requires. Flag for a lawyer: (a) whether the service could be an "interpersonal communications service" under the Act (it is not real-time and not addressed to a recipient by the provider, so this seems unlikely, but § 2 was not analysed in full); (b) whether the new Czech Cybersecurity Act (zákon č. 264/2025 Sb., effective 1 Nov 2025 per press reports; not opened in this session) lists any service type that could catch this operator.

- Claim: GDPR Art 5(1)(e) storage limitation.
  URL: https://www.legislation.gov.uk/eur/2016/679/article/5/adopted
  Quote: "kept in a form which permits identification of data subjects for no longer than is necessary for the purposes"
  Status: PARTIAL (official mirror; EUR-Lex blocked)

- Claim: GDPR Art 32(1) security, naming pseudonymisation and encryption as measures.
  URL: https://www.legislation.gov.uk/eur/2016/679/article/32/adopted
  Quote: "the controller and the processor shall implement appropriate technical and organisational measures" / "the pseudonymisation and encryption of personal data;"
  Status: PARTIAL (official mirror)

Reasoning: The combination of Breyer (legitimate interest in short IP logging against attacks), Art 5(1)(e) and Art 32 supports a policy of short, purpose-bound logs (days, not months) with truncated or hashed IPs, and no obligation to keep more. No positive retention duty was found for this kind of service.

---

## 6. Abuse handling for an encrypted service

- Claim: Mozilla shut Firefox Send (an encrypted file-sharing service) in 2020 after malware and phishing abuse.
  URL: https://blog.mozilla.org/en/products/firefox/update-on-firefox-send-and-firefox-notes/ (17 Sept 2020)
  Quote: "some abusive users were beginning to use Send to ship malware and conduct spear phishing attacks."
  Status: VERIFIED

- Claim: DSA Art 7 lets a provider do voluntary checks without losing the Art 6 exemption; Art 8 bars general monitoring. (Quotes in section 4.)
  Status: PARTIAL (mirror)

- Claim: The DSA contains a recital on encryption or end-to-end encrypted services.
  URL tried: https://www.eu-digital-services-act.com/Digital_Services_Act_Preamble_21_to_30.html, ..._31_to_40.html, ..._41_to_50.html
  Status: NOT FOUND in recitals 21-50 (no "encrypt" match); recitals 1-20 and 51-156 not searched; EUR-Lex blocked. The encryption-protective language found online belongs to Parliament's 2021 amendments or the separate CSAM proposal, not the adopted DSA text.

Reasoning: With ciphertext only, the operator can act on notices by link id (delete the object, block the id, rate-limit the source IP range), cannot hash-match content, and should say so in the Art 14 terms and the Art 16 notice form. Art 7 means a voluntary scan of what the operator can see (sizes, upload patterns, link-id abuse reports) does not forfeit the Art 6 exemption.

---

## 7. Cookies and ePrivacy with no cookies, functional localStorage only

- Claim: Art 5(3) ePrivacy Directive (as amended by 2009/136/EC) covers storing or accessing information on terminal equipment, with a strictly-necessary exemption.
  URL: https://www.legislation.gov.uk/eudr/2002/58/article/5 (revised text, Art 5(3) marked as substituted by 2009/136/EC)
  Quote: "the storing of information, or the gaining of access to information already stored, in the terminal equipment of a subscriber or user is only allowed on condition"
  Quote: "strictly necessary in order for the provider of an information society service explicitly requested by the subscriber or user to provide the service."
  Status: PARTIAL (official UK mirror; EUR-Lex blocked)

- Claim: EDPB Guidelines 2/2023 (v2.0, adopted 7 Oct 2024) treat local storage like cookies: writing to the device is "storage"; reading it back off the device is "gaining access"; purely on-device use that never leaves the device is not.
  URL: https://edpb.europa.eu/system/files/2024-10/edpb_guidelines_202302_technical_scope_art_53_eprivacydirective_v2_en_0.pdf
  Quote (para 44): "such as cookies, local storage, WebSQL" ... "would not constitute a 'gaining of access to information already stored' ... as long as the information does not leave the device"
  Quote (para 35): "Storage of information in the sense of Article 5(3) ePD refers to placing information on a physical electronic storage medium"
  Status: VERIFIED

- Claim: The guidelines do not decide the exemptions; each use must be checked against them.
  URL: same, para 40
  Quote: "it would have to be assessed if a consent is needed or whether an exemption under Article 5(3) ePD could apply"
  Status: VERIFIED

Reasoning: Writing a UI preference or a decryption key fragment to localStorage is "storage" under Art 5(3). If it is never sent back to the server and exists only to deliver the function the user asked for, it falls under the strictly-necessary exemption and needs no consent banner. Anything read from localStorage and transmitted (analytics IDs, fingerprints) would need consent. The Czech transposition (§ 89(3) of the same Act 127/2005 Sb.) was not opened; flag for a lawyer only if the service starts storing anything beyond functional state.

---

## 8. Scope checklist

Sources for the vendor facts in this table: Sentry https://docs.sentry.io/organization/data-storage-location/ ("Sentry supports data storage in both the US and the EU."; EU = Frankfurt; account, org settings, audit logs and support data stay in the US; "once selected, your data storage location can't be changed.") VERIFIED. Better Stack https://betterstack.com/privacy ("Better Stack, Inc., a Delaware corporation"; "We process personal data primarily in the European Union.") and https://betterstack.com/community/blog/changelog-9-advanced-data-options-for-everyone/ ("Choose from US East, US West, Germany, and Singapore.") VERIFIED; DPA relies on DPF. Uptime Kuma https://github.com/louislam/uptime-kuma ("A fancy self-hosted monitoring tool", MIT) VERIFIED. deSEC: entity "deSEC e.V.", Möckernstraße 74, 10965 Berlin, "AG Berlin (Charlottenburg), VR 37525", recognised as charitable, from the site's source file https://raw.githubusercontent.com/desec-io/desec-stack/main/www/webapp/src/views/ImpressumPage.vue (the live site is a JS app and returned only its title) PARTIAL; a community thread notes some anycast frontends are in the US (https://talk.desec.io/t/hosted-in-the-us/1437, secondary). Let's Encrypt https://letsencrypt.org/privacy/ (ISRG is "a California (United States) Nonprofit Public Benefit Corporation"; collects "the IP addresses from which you access the Let's Encrypt service"; "We will store this information for a minimum of two years per trusted root program requirements.") VERIFIED. Hetzner, Scaleway, Cloudflare, Oracle, Vercel: section 3.

| Component | Does "EU hosting" plausibly require EU? | What it sees | Cheapest EU-compliant option found | Notes |
|---|---|---|---|---|
| App compute | Yes (core of the promise) | Ciphertext, link ids, IPs, timestamps | Hetzner Cloud (fsn1/nbg1/hel1) or Scaleway (PAR/AMS/WAW) | Both EU-domiciled; Hetzner DPA limits processing to EU/EEA. Cloudflare Workers run in-region only with the Enterprise add-on and code is replicated globally. Vercel Hobby = one region, US entity, US primary processing. |
| Database (metadata: link id, expiry, size) | Yes | Link ids, expiry, maybe hashed IP | Same VM (SQLite/Postgres) on Hetzner/Scaleway | Cloudflare D1/KV have no jurisdictional restriction option. |
| Object storage (ciphertext) | Yes, by the narrow reading; by the broad reading the ciphertext alone is low-risk | Ciphertext only | Hetzner Object Storage (EU locations) or Scaleway Object Storage; Cloudflare R2 with `eu` jurisdiction if staying on Cloudflare | R2 `eu` guarantees storage location but Cloudflare, Inc. remains the US contracting party. |
| Backups | Yes (same data as above) | Everything above | Hetzner: "backups also remain within the EU" when an EU location is chosen | Keep backups short-lived; the data is one-time by design. |
| Logs / metrics | Yes (this is where personal data actually lives) | IPs, user agents, link ids, timestamps | Local files or self-hosted stack on the same EU VM; Better Stack Germany region if SaaS | Cloudflare Workers Logs give no region; Customer Metadata Boundary is Enterprise. Better Stack is a US (Delaware) entity. |
| Error tracking | Yes if events carry IPs or link ids; scrub them and the question softens | Stack traces, request metadata | Self-hosted GlitchTip or Sentry self-hosted on the EU VM; Sentry SaaS EU region (Frankfurt) as a paid option | Sentry EU region still keeps account, org settings, audit logs and support tickets in the US. |
| Uptime monitoring | Weak requirement: the monitor only sees that the public URL responds | Public URL, response codes, TLS info | Uptime Kuma self-hosted (MIT) on a second cheap EU VM | External checks from US probes are not a transfer of user data. |
| DNS | Weak: authoritative DNS sees resolver IPs, not users' content | Resolver IPs, query names | deSEC (Berlin e.V., free) or Hetzner DNS | deSEC keys are held in Germany; some anycast frontends are outside the EU (secondary source). |
| TLS certificates | Not required: the CA sees only domain names and the ACME client's IP | Domain name, operator's server IP | Let's Encrypt (free) | US nonprofit; data may be stored in the US; retention 2+ years. Alternative EU ACME CAs exist but were not researched. |
| CDN for static assets | Not required for JS/CSS (no user data); required if the CDN proxies uploads/downloads | If proxying: IPs and ciphertext in transit | Serve statics from the EU VM; if using Cloudflare, DNS-only (grey cloud) for the upload/download host | Proxying through Cloudflare on Free/Pro terminates TLS at the nearest PoP, which may be outside the EU. |
| Email for abuse contact (Art 12 DSA point of contact) | Weak: it sees reporters' emails and link ids | Reporter identity, link ids | Any EU mailbox provider (e.g. a German or Czech provider); not verified in this session | mailbox.org states Berlin data centres but no Germany-only guarantee on the page opened. |
| Source hosting on GitHub | Not required: no user data; GitHub is US-owned | Source code, contributor identities | Keep GitHub; optionally mirror to Codeberg (Berlin e.V.; not verified here) | Out of scope of "hosting the public instance". |

---

## Needs a lawyer

1. DSA classification: confirm the service is "hosting" (Art 3(g)(iii)) and not an "online platform" (Art 3(i)); this fixes which obligations apply and whether Art 19 matters at all.
2. Whether Arts 11, 12, 14, 16, 17 DSA can be satisfied by a single email address plus a page in the repo for a one-person operator, and what "expeditiously" means for link-id takedowns.
3. Whether any Czech transposition (zákon o digitální ekonomice, once adopted) adds national duties or fees for intermediaries.
4. GDPR controller analysis for ciphertext after EDPS v SRB: whether the operator can treat the blob as non-personal data for its own purposes while treating logs and link ids as personal data.
5. Transfer impact assessment template if any US-owned SaaS (Cloudflare, Sentry EU, Better Stack) stays in the stack, with the C-703/25 P appeal as a monitored risk.
6. Whether the Czech Electronic Communications Act (number-independent interpersonal communications service) or the 2025 Cybersecurity Act could catch this service; both look unlikely but were not analysed in full.
7. Czech ePrivacy transposition (§ 89(3) of Act 127/2005 Sb.) for functional localStorage, if the UI ever stores more than functional state.
8. Terms of service and privacy notice wording for an anonymous service (no accounts, IP logs, retention periods, Art 13/14 GDPR information duties).

---

## Decision-relevant summary

- IP addresses in access logs are personal data for the operator (Breyer); that, not the ciphertext, is what makes GDPR transfer rules bite.
- After EDPS v SRB (Sept 2025), ciphertext the operator cannot decrypt is arguably not personal data from the operator's side, but logs and link ids are, and they can be joined.
- The EU-US DPF adequacy decision is valid today; the General Court upheld it on 3 Sept 2025 and the appeal C-703/25 P is pending without suspensive effect.
- EDPB/EDPS position since 2019: a US CLOUD Act demand is not a GDPR transfer basis; the answer is MLAT. The exposure is the vendor's US jurisdiction, not the rack location.
- A US-owned provider's EU region satisfies "data at rest in the EU" but not "under EU jurisdiction only"; their DPAs reserve processing outside the EEA (Cloudflare) or state primary processing in the US (Vercel).
- Hetzner (Germany) and Scaleway (France) are EU-domiciled; Hetzner's DPA limits processing to the EU/EEA and keeps backups and support in the EU; Scaleway claims CLOUD Act immunity but SecNumCloud is still in progress.
- Cloudflare's data localisation (Regional Services, Customer Metadata Boundary) is an Enterprise add-on; on cheaper plans Workers may run and logs may be stored anywhere; R2 has a free-standing `eu` jurisdiction.
- Under the DSA the service is most likely a "hosting service"; Arts 11, 12, 14, 16, 17 apply with no size exemption; Art 19's micro/small waiver is for online platforms only; Art 15 reporting is waived.
- Czech DSA enforcement is ČTÚ, but it cannot inspect or fine until the digital economy act passes; the bill was still in the Chamber on 21 Oct 2026 and the Commission has referred Czechia to the CJEU.
- No EU or Czech law was found that forces a small hosting service to retain access logs; the 6-month Czech duty applies to electronic communications providers, which exclude content services. GDPR Art 5(1)(e) and Art 32 point to minimal, short logs.
- Firefox Send shows abuse pressure is the realistic failure mode; notice-and-action by link id plus DSA Art 7 voluntary checks is the available toolkit.
- Functional localStorage is "storage" under ePrivacy Art 5(3) but fits the strictly-necessary exemption if nothing is read back and transmitted; no consent banner needed on that basis.

---

## URLs opened

Primary legal
- https://curia.europa.eu/jcms/upload/docs/application/pdf/2016-10/cp160112en.pdf
- https://curia.europa.eu/jcms/upload/docs/application/pdf/2025-09/cp250107en.pdf
- https://curia.europa.eu/jcms/upload/docs/application/pdf/2025-09/cp250106en.pdf
- https://www.edpb.europa.eu/documents/edpb-correspondence/edpb-edps-joint-response-to-the-libe-committee-on-the-impact-of-the_en
- https://www.edpb.europa.eu/system/files/documents/files/file1/edpb_edps_joint_response_us_cloudact_coverletter.pdf
- https://www.edpb.europa.eu/system/files/documents/files/file2/edpb_edps_joint_response_us_cloudact_annex.pdf
- https://edpb.europa.eu/system/files/2024-10/edpb_guidelines_202302_technical_scope_art_53_eprivacydirective_v2_en_0.pdf
- https://commission.europa.eu/law/law-topic/data-protection/international-dimension-data-protection/standard-contractual-clauses-scc_en
- https://digital-strategy.ec.europa.eu/en/policies/dsa-dscs
- https://www.legislation.gov.uk/eur/2016/679/article/4/adopted
- https://www.legislation.gov.uk/eur/2016/679/article/5/adopted
- https://www.legislation.gov.uk/eur/2016/679/article/32/adopted
- https://www.legislation.gov.uk/eur/2016/679/article/44/adopted
- https://www.legislation.gov.uk/eur/2016/679/article/48/adopted
- https://www.legislation.gov.uk/eudr/2002/58/article/5
- https://www.zakonyprolidi.cz/cs/2005-127
- https://www.psp.cz/sqw/historie.sqw?o=10&t=69
- https://ctu.gov.cz/sites/default/files/obsah/soubory-ke-stazeni/Let%C3%A1k_DSA.pdf
- http://digimajak.ctu.gov.cz/

DSA text (third-party reproduction)
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_3.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_6.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_7.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_8.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_11.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_12.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_14.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_15.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_16.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_17.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Article_19.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Preamble_21_to_30.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Preamble_31_to_40.html
- https://www.eu-digital-services-act.com/Digital_Services_Act_Preamble_41_to_50.html

Vendors
- https://www.cloudflare.com/data-localization/
- https://www.cloudflare.com/cloudflare-customer-dpa/
- https://developers.cloudflare.com/data-localization/
- https://developers.cloudflare.com/data-localization/how-to/workers/
- https://developers.cloudflare.com/data-localization/metadata-boundary/
- https://developers.cloudflare.com/data-localization/regional-services/
- https://developers.cloudflare.com/data-localization/compatibility/
- https://developers.cloudflare.com/r2/reference/data-location/
- https://developers.cloudflare.com/workers/observability/logs/workers-logs/
- https://www.oracle.com/a/ocom/docs/ocloud-hosting-delivery-policies-3089853.pdf
- https://docs.oracle.com/en-us/iaas/Content/General/Concepts/regions.htm
- https://www.theregister.com/2022/07/13/oracle_eu_sovereign_cloud/
- https://www.scaleway.com/en/legal-notice/
- https://www.scaleway.com/en/security-and-compliance/
- https://www.scaleway.com/en/blog/secnumcloud-qualification-trusted-cloud/
- https://www.scaleway.com/en/docs/account/reference-content/products-availability/
- https://www.scaleway.com/en/subprocessorlist/
- https://www.scaleway.com/en/contracts/
- https://www.hetzner.com/legal/legal-notice/
- https://www.hetzner.com/unternehmen/rechenzentrum/
- https://docs.hetzner.com/cloud/general/locations/
- https://docs.hetzner.com/general/company-and-policy/data-protection-at-hetzner/
- https://www.hetzner.com/AV/DPA_en.pdf
- https://vercel.com/legal/dpa
- https://vercel.com/docs/functions/configuring-functions/region
- https://docs.sentry.io/organization/data-storage-location/
- https://betterstack.com/privacy
- https://betterstack.com/dpa
- https://betterstack.com/community/blog/changelog-9-advanced-data-options-for-everyone/
- https://github.com/louislam/uptime-kuma
- https://raw.githubusercontent.com/desec-io/desec-stack/main/www/webapp/src/views/ImpressumPage.vue
- https://talk.desec.io/t/hosted-in-the-us/1437
- https://letsencrypt.org/privacy/
- https://mailbox.org/en/about-us/
- https://blog.mozilla.org/en/products/firefox/update-on-firefox-send-and-firefox-notes/

Commentary and trackers
- https://digitalpolicyalert.org/event/35459-latombe-filed-appeal-against-general-court-dismissal-of-challenge-to-european-unionunited-states-data-protection-framework-adequacy-decision-in-latombe-v-commission
- https://www.wilmerhale.com/en/insights/blogs/wilmerhale-privacy-and-cybersecurity-law/20251201-european-court-of-justice-to-review-challenge-to-eu-us-data-privacy-framework
- https://eucrim.eu/documentation/ecj-eu-criminal-law-cases-overview/t-553-23/
- https://www.lupa.cz/aktuality/evropska-komise-zaluje-cesko-kvuli-neprijeti-pravomoci-pro-digitalniho-regulatora/

Tried and blocked or empty (not relied on)
- EUR-Lex: all URLs for 2022/2065, 2016/679, 2021/914 and C/2025/6610 (AWS WAF challenge)
- https://curia.europa.eu/juris/liste.jsf?num=C-703/25 and the infocuria redirect (JS only)
- https://www.oracle.com/cloud/sovereign-cloud/eu-sovereign-cloud/ and Oracle blog posts (403)
- https://www.scaleway.com/en/gdpr/ and /en/sovereignty/ (404)
- https://www.hetzner.com/legal/data-processing-agreement/ (404; the PDF above was used instead)
- https://desec.io/ , /about , /privacy-policy (JS app; title only)
- https://betterstack.com/subprocessors (404)
- https://captaincompliance.com/... and https://ieu-monitoring.com/... (403)
- https://www.eu-digital-services-act.com/Digital_Services_Act_Recital_30.html (404; preamble pages used instead)
- lexparency.org (certificate error)
