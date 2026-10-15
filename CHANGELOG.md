## 0.1.0 (Unreleased)

FEATURES:

* **New Resource:** `simplelogin_alias`: create, update, import and delete aliases with exact addresses on verified custom domains, managing forwarding mailboxes, note, name and enabled state.
* `simplelogin_alias`: `on_destroy = "disable"` disables the alias on destroy or replacement instead of deleting it.
* **New Data Source:** `simplelogin_alias`: read an alias by ID.
* **New Data Source:** `simplelogin_mailbox`: look up a mailbox ID by email address.
* **New Data Source:** `simplelogin_domain`: read a custom domain by name.
