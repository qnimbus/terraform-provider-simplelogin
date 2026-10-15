data "simplelogin_mailbox" "primary" {
  email = "personal@example.net"
}

output "primary_mailbox_id" {
  value = data.simplelogin_mailbox.primary.id
}
