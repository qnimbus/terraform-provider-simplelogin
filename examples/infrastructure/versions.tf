terraform {
  required_version = ">= 1.6.0"

  required_providers {
    simplelogin = {
      # Full address on purpose: OpenTofu expands a short "qnimbus/simplelogin"
      # to registry.opentofu.org/qnimbus/simplelogin, which is a different
      # provider address than the one this provider is released under.
      source  = "registry.terraform.io/qnimbus/simplelogin"
      version = "~> 0.1.0"
    }
  }
}

# The API key is read from SIMPLELOGIN_API_KEY.
provider "simplelogin" {}
