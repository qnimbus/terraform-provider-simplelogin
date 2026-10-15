data "simplelogin_domain" "example" {
  domain = "example.com"
}

output "example_domain_verified" {
  value = data.simplelogin_domain.example.is_verified
}
