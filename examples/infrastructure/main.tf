# SimpleLogin aliases managed by OpenTofu.
#
# DNS for the alias domain (MX, SPF, DKIM, DMARC and the SimpleLogin
# verification record) is owned exclusively by DNSControl. Nothing here
# manages DNS records.

variable "alias_domain" {
  description = "Verified SimpleLogin custom domain the aliases live on."
  type        = string
  default     = "example.com"
}

variable "primary_mailbox" {
  description = "Mailbox that receives forwarded mail."
  type        = string
  default     = "personal@example.net"
}

data "simplelogin_domain" "main" {
  domain = var.alias_domain
}

data "simplelogin_mailbox" "primary" {
  email = var.primary_mailbox
}

resource "simplelogin_alias" "admin" {
  email = "admin@${data.simplelogin_domain.main.domain}"

  mailbox_ids = [
    data.simplelogin_mailbox.primary.id,
  ]

  note    = "Administrative email alias"
  enabled = true

  lifecycle {
    prevent_destroy = true
  }
}

output "admin_alias_id" {
  value = simplelogin_alias.admin.id
}
