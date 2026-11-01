terraform {
  required_version = "= 1.13.2"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "= 5.28.1"
    }
  }
  encryption {
    state { enforced = true }
    plan { enforced = true }
  }
}

provider "cloudflare" {}

variable "account_id" { type = string }
variable "zone" { type = string }
variable "domain" { type = string }
variable "ipv4_address" { type = string }
variable "ipv6_address" { type = string }
variable "acme_account_uri" {
  type    = string
  default = ""
  validation {
    condition     = var.acme_account_uri == "" || can(regex("^https://acme-v02\\.api\\.letsencrypt\\.org/acme/acct/[1-9][0-9]*$", var.acme_account_uri))
    error_message = "Use the Let's Encrypt account URL from the server's Caddy storage."
  }
}

locals {
  apex = var.domain == var.zone
  certificate_issuer = join("; ", compact([
    "letsencrypt.org",
    "validationmethods=http-01",
    var.acme_account_uri == "" ? "" : "accounturi=${var.acme_account_uri}",
  ]))
}

resource "cloudflare_zone" "zone" {
  account = { id = var.account_id }
  name    = var.zone
  type    = "full"
  lifecycle { prevent_destroy = true }
}

resource "cloudflare_zone_dnssec" "zone" {
  zone_id = cloudflare_zone.zone.id
  status  = "active"
  lifecycle { prevent_destroy = true }
}

resource "cloudflare_universal_ssl_setting" "zone" {
  zone_id = cloudflare_zone.zone.id
  enabled = false
}

resource "cloudflare_dns_record" "addresses" {
  for_each = { A = var.ipv4_address, AAAA = var.ipv6_address }
  zone_id  = cloudflare_zone.zone.id
  name     = var.domain
  type     = each.key
  content  = each.value
  ttl      = 300
  proxied  = false
}

resource "cloudflare_dns_record" "certificate_authority" {
  zone_id = cloudflare_zone.zone.id
  name    = var.domain
  type    = "CAA"
  ttl     = 300
  data = {
    flags = 0
    tag   = "issue"
    value = local.certificate_issuer
  }
}

resource "cloudflare_dns_record" "no_wildcard_certificates" {
  zone_id = cloudflare_zone.zone.id
  name    = var.domain
  type    = "CAA"
  ttl     = 300
  data = {
    flags = 0
    tag   = "issuewild"
    value = ";"
  }
}

resource "cloudflare_dns_record" "www" {
  count   = local.apex ? 1 : 0
  zone_id = cloudflare_zone.zone.id
  name    = "www.${var.domain}"
  type    = "CNAME"
  content = var.domain
  ttl     = 300
  proxied = false
}

resource "cloudflare_dns_record" "null_mail_exchanger" {
  zone_id  = cloudflare_zone.zone.id
  name     = var.domain
  type     = "MX"
  content  = "."
  priority = 0
  ttl      = 300
}

resource "cloudflare_dns_record" "no_mail_senders" {
  zone_id = cloudflare_zone.zone.id
  name    = var.domain
  type    = "TXT"
  content = "\"v=spf1 -all\""
  ttl     = 300
}

resource "cloudflare_dns_record" "reject_spoofed_mail" {
  zone_id = cloudflare_zone.zone.id
  name    = "_dmarc.${var.domain}"
  type    = "TXT"
  content = "\"v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s\""
  ttl     = 300
}

output "name_servers" { value = cloudflare_zone.zone.name_servers }
output "registrar_ds_record" { value = cloudflare_zone_dnssec.zone.ds }
output "zone_id" { value = cloudflare_zone.zone.id }
output "certificate_authority_record" { value = local.certificate_issuer }
