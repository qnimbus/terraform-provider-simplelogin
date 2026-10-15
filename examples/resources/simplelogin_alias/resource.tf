data "simplelogin_mailbox" "primary" {
  email = "personal@example.net"
}

resource "simplelogin_alias" "admin" {
  email       = "admin@example.com"
  mailbox_ids = [data.simplelogin_mailbox.primary.id]

  note    = "Administrative email alias"
  enabled = true

  # Deleting an alias moves its address to the SimpleLogin domain trash, and the
  # same address cannot be created again. Protect addresses you depend on.
  lifecycle {
    prevent_destroy = true
  }
}
