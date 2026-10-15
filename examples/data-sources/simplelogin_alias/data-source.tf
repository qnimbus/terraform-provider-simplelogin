data "simplelogin_alias" "admin" {
  id = "12345"
}

output "admin_alias_enabled" {
  value = data.simplelogin_alias.admin.enabled
}
