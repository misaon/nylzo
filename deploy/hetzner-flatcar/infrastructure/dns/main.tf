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

resource "cloudflare_zone_dns_settings" "zone" {
  zone_id   = cloudflare_zone.zone.id
  zone_mode = "dns_only"
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
    value = "letsencrypt.org"
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

output "name_servers" { value = cloudflare_zone.zone.name_servers }
output "registrar_ds_record" { value = cloudflare_zone_dnssec.zone.ds }
output "zone_id" { value = cloudflare_zone.zone.id }
