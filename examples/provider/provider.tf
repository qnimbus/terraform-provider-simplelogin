terraform {
  required_providers {
    simplelogin = {
      # Always use the full address: in OpenTofu a short "qnimbus/simplelogin"
      # would resolve to registry.opentofu.org instead.
      source  = "registry.terraform.io/qnimbus/simplelogin"
      version = "~> 0.1"
    }
  }
}

# Reads the API key from SIMPLELOGIN_API_KEY.
provider "simplelogin" {}
