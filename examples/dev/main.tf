# Smoke-test configuration for the locally built provider (make dev-plan).
# It makes no API calls during plan, so any non-empty API key works.
terraform {
  required_providers {
    simplelogin = {
      source = "registry.terraform.io/qnimbus/simplelogin"
    }
  }
}

provider "simplelogin" {}

variable "mailbox_id" {
  type    = number
  default = 1
}

resource "simplelogin_alias" "smoke" {
  email       = "smoke-test@example.com"
  mailbox_ids = [var.mailbox_id]
  note        = "local development smoke test"
}
