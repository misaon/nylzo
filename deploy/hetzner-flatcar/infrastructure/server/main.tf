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
variable "image_id" { type = number }
variable "flatcar_version" { type = string }
variable "architecture" { type = string }
variable "user_data_path" { type = string }
variable "administrator_networks" { type = list(string) }

data "hcloud_image" "flatcar" { id = var.image_id }

resource "hcloud_firewall" "server" {
  name   = var.name
  labels = { application = "nylzo" }
  dynamic "rule" {
    for_each = ["80", "443"]
    content {
      direction  = "in"
      protocol   = "tcp"
      port       = rule.value
      source_ips = ["0.0.0.0/0", "::/0"]
    }
  }
  rule {
    direction  = "in"
    protocol   = "udp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction  = "in"
    protocol   = "icmp"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = var.administrator_networks
  }
}

resource "hcloud_server" "server" {
  name               = var.name
  location           = var.location
  server_type        = var.server_type
  image              = var.image_id
  user_data          = file(var.user_data_path)
  firewall_ids       = [hcloud_firewall.server.id]
  backups            = false
  delete_protection  = true
  rebuild_protection = true
  labels             = { application = "nylzo", profile = "flatcar" }
  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = lookup(data.hcloud_image.flatcar.labels, "nylzo-verified-image", "") == "true" && lookup(data.hcloud_image.flatcar.labels, "nylzo-flatcar-version", "") == var.flatcar_version && lookup(data.hcloud_image.flatcar.labels, "nylzo-architecture", "") == var.architecture
      error_message = "Use an image produced by the verified image factory in this account."
    }
  }
  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }
}

output "ipv4_address" { value = hcloud_server.server.ipv4_address }
output "ipv6_address" { value = hcloud_server.server.ipv6_address }
output "server_id" { value = hcloud_server.server.id }
