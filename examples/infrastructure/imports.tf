# Adopting aliases that already exist in SimpleLogin.
#
# 1. Find the alias ID (open the alias in the SimpleLogin dashboard, or use
#    the API: GET /api/v2/aliases?page_id=0).
# 2. Add an import block like the one below, with a resource block whose
#    attributes match the alias as it is today.
# 3. Run `tofu plan`: it must show "1 to import, 0 to add, 0 to change,
#    0 to destroy". Adjust the resource block until it does.
# 4. Run `tofu apply`. Importing never modifies the alias.
#
# import {
#   to = simplelogin_alias.admin
#   id = "12345"
# }
