terraform {
  required_version = "= 1.13.2"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "= 1.70.2"
    }
  }
  encryption {
    state { enforced = true }
    plan { enforced = true }
  }
}

provider "hcloud" {}

variable "name" { type = string }
variable "location" { type = string }
variable "server_type" { type = string }
variable "user_data_path" { type = string }

resource "hcloud_firewall" "builder" {
  name   = "${var.name}-image-builder"
  labels = { application = "nylzo", purpose = "image-builder" }
}

resource "hcloud_server" "builder" {
  name         = "${var.name}-image-builder"
  location     = var.location
  server_type  = var.server_type
  image        = "ubuntu-24.04"
  user_data    = file(var.user_data_path)
  firewall_ids = [hcloud_firewall.builder.id]
  labels       = { application = "nylzo", purpose = "image-builder" }
  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }
}

output "server_id" { value = hcloud_server.builder.id }
