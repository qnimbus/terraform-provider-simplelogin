// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/qnimbus/terraform-provider-simplelogin/internal/client"
)

var noLineBreaks = regexp.MustCompile(`^[^\r\n]*$`)

func parseAliasID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("alias ID must be a positive integer, got %q", s)
	}
	return id, nil
}

// nullableString maps API null and "" to a null value, so that an unset
// optional attribute never produces a diff.
func nullableString(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func int64Set(ids []int64) types.Set {
	elems := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elems = append(elems, types.Int64Value(id))
	}
	return types.SetValueMust(types.Int64Type, elems)
}

// int64sFromSet returns the set's elements in ascending order.
func int64sFromSet(ctx context.Context, s types.Set) ([]int64, diag.Diagnostics) {
	var ids []int64
	diags := s.ElementsAs(ctx, &ids, false)
	slices.Sort(ids)
	return ids, diags
}

// aliasEmailValidator rejects addresses that SimpleLogin would normalise into
// a different address, so problems surface at plan time.
type aliasEmailValidator struct{}

func (aliasEmailValidator) Description(context.Context) string {
	return "must be an alias address with a lowercase local part of 1-40 characters [a-z0-9._-]"
}

func (v aliasEmailValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (aliasEmailValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, _, err := client.SplitAliasEmail(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid alias address", err.Error())
	}
}
