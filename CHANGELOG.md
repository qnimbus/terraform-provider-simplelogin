## 0.1.0 (October 15, 2026)

Initial release.

FEATURES:

* **New Resource:** `simplelogin_alias`: create, update, import and delete aliases with exact addresses on verified custom domains, managing forwarding mailboxes, note, name and enabled state.
* `simplelogin_alias`: `on_destroy = "disable"` disables the alias on destroy or replacement instead of deleting it.
* **New Data Source:** `simplelogin_alias`: read an alias by ID.
* **New Data Source:** `simplelogin_mailbox`: look up a mailbox ID by email address.
* **New Data Source:** `simplelogin_domain`: read a custom domain by name.

NOTES:

* Distributed as GitHub release archives for linux_amd64, linux_arm64 and windows_amd64, installed through an OpenTofu filesystem mirror (see README). Not published to a provider registry.
* Deleting a custom-domain alias moves its address to the domain trash, which blocks re-creation until it is removed there. Use `prevent_destroy` or `on_destroy = "disable"` for addresses you depend on.
