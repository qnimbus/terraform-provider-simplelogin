// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/qnimbus/terraform-provider-simplelogin/internal/client"
)

var (
	_ resource.Resource                = &aliasResource{}
	_ resource.ResourceWithConfigure   = &aliasResource{}
	_ resource.ResourceWithImportState = &aliasResource{}
)

// NewAliasResource returns the simplelogin_alias resource.
func NewAliasResource() resource.Resource { return &aliasResource{} }

type aliasResource struct {
	client *client.Client
}

type aliasResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Email      types.String `tfsdk:"email"`
	MailboxIDs types.Set    `tfsdk:"mailbox_ids"`
	Note       types.String `tfsdk:"note"`
	Name       types.String `tfsdk:"name"`
	Enabled    types.Bool   `tfsdk:"enabled"`
}

func (r *aliasResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alias"
}

func (r *aliasResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a SimpleLogin alias with an exact address on a verified custom domain, " +
			"including its forwarding mailboxes, note, display name and enabled state.\n\n" +
			"~> **Deletion is effectively irreversible.** When an alias on a custom domain is deleted, SimpleLogin moves the address " +
			"to the domain's trash and refuses to create the same address again while it is there. Destroying this resource, or changing `email` " +
			"(which forces replacement), deletes the alias. Use `lifecycle { prevent_destroy = true }` for addresses you depend on.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "SimpleLogin alias ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "Full alias address, e.g. `admin@example.com`. The domain must be a verified custom domain of the account. " +
					"The local part must be 1-40 characters of lowercase `a-z`, `0-9`, `.`, `_` or `-`, matching what SimpleLogin accepts. " +
					"Changing this forces a new alias to be created and the old one to be **deleted**.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{aliasEmailValidator{}},
			},
			"mailbox_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of the verified mailboxes that receive mail for this alias. At least one is required. " +
					"Use the `simplelogin_mailbox` data source to look up IDs.",
				ElementType: types.Int64Type,
				Required:    true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueInt64sAre(int64validator.AtLeast(1)),
				},
			},
			"note": schema.StringAttribute{
				MarkdownDescription: "Free-form note. Omit to keep the alias without a note; an empty string is not allowed.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name used as the sender name when replying from the alias. At most 128 characters, no line breaks. " +
					"Omit to leave it unset.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 128),
					stringvalidator.RegexMatches(noLineBreaks, "must not contain line breaks"),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the alias forwards mail. Mail sent to a disabled alias is blocked instead of forwarded. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
		},
	}
}

func (r *aliasResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.client = c
}

func (r *aliasResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan aliasResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	email := plan.Email.ValueString()
	prefix, domain, err := client.SplitAliasEmail(email)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("email"), "Invalid alias address", err.Error())
		return
	}
	mailboxIDs, diags := int64sFromSet(ctx, plan.MailboxIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Signed suffixes expire after ~10 minutes upstream, so resolve them
	// immediately before creating the alias.
	opts, err := r.client.GetAliasOptions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read SimpleLogin alias options", err.Error())
		return
	}
	if !opts.CanCreate {
		resp.Diagnostics.AddError("SimpleLogin account cannot create aliases",
			"SimpleLogin reports that this account cannot create new aliases, typically because the plan's alias quota is reached.")
		return
	}
	suffix, err := client.FindExactSuffix(opts, domain)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("email"), "Alias address cannot be created", err.Error())
		return
	}

	created, err := r.client.CreateCustomAlias(ctx, client.CreateAliasRequest{
		AliasPrefix:  prefix,
		SignedSuffix: suffix.SignedSuffix,
		MailboxIDs:   mailboxIDs,
		Note:         plan.Note.ValueStringPointer(),
		Name:         plan.Name.ValueStringPointer(),
	})
	if err != nil {
		var apiErr *client.APIError
		switch {
		case client.IsConflict(err):
			resp.Diagnostics.AddAttributeError(path.Root("email"), "Alias already exists",
				fmt.Sprintf("SimpleLogin refused to create %s because the address already exists or was deleted before "+
					"(deleted custom-domain aliases stay in the domain trash and block the address).\n\n"+
					"To manage an existing alias, import it instead of creating it:\n\n"+
					"  import {\n    to = simplelogin_alias.<name>\n    id = \"<alias id>\"\n  }\n\n"+
					"If the address was deleted, it is blocked by the custom domain's trash (dashboard: domain settings, deleted aliases). "+
					"Removing it from the trash frees the address so it can be created again.\n\n%s", email, err))
		case errors.As(err, &apiErr):
			resp.Diagnostics.AddError("Failed to create SimpleLogin alias", err.Error())
		default:
			resp.Diagnostics.AddError("SimpleLogin alias creation outcome unknown",
				fmt.Sprintf("The request to create %s failed without a response from SimpleLogin, so the alias may or may not have been created. "+
					"The request is not retried automatically. Check the SimpleLogin dashboard: if the alias exists, import it; otherwise apply again.\n\n%s", email, err))
		}
		return
	}

	if created.Email != email {
		// Track what was created so OpenTofu marks it tainted and replaces it,
		// rather than leaving an unmanaged alias with the wrong address behind.
		resp.Diagnostics.Append(resp.State.Set(ctx, aliasModelFromAPI(created))...)
		resp.Diagnostics.AddError("SimpleLogin created a different address",
			fmt.Sprintf("Requested %s but SimpleLogin created %s (alias ID %d). The alias has been recorded in state as tainted; "+
				"the next apply will delete it and try again. Please report this issue.", email, created.Email, created.ID))
		return
	}

	// From here on the alias exists. Any error returned from Create would make
	// OpenTofu taint the resource and replace it, i.e. delete the new alias,
	// whose address is then blocked by the domain trash. Later failures are
	// therefore reported as warnings; the refresh before the next plan reads
	// the real values and plans an in-place update if anything is off.
	model := aliasModelFromAPI(created)

	// New aliases are always enabled.
	if !plan.Enabled.ValueBool() {
		if diags := r.ensureEnabled(ctx, created.ID, created.Enabled, false); diags.HasError() {
			// Record the planned value: a state that differs from the plan
			// would also taint the resource.
			model.Enabled = plan.Enabled
			resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
			resp.Diagnostics.AddWarning("SimpleLogin alias created but not disabled",
				fmt.Sprintf("Alias %s (ID %d) was created, but disabling it failed, so it is currently ENABLED and forwards mail. "+
					"The next plan will detect this and disable it in place.\n\n%s", email, created.ID, summarize(diags)))
			return
		}
		model.Enabled = types.BoolValue(false)
	}

	if a, err := r.client.GetAlias(ctx, created.ID, email); err != nil {
		resp.Diagnostics.AddWarning("SimpleLogin alias created but could not be read back",
			fmt.Sprintf("Alias %s (ID %d) was created. Reading it back failed, so state is based on the create response; "+
				"the next refresh will correct any difference.\n\n%s", email, created.ID, err))
	} else {
		model = aliasModelFromAPI(a)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

// summarize joins the details of error diagnostics, for embedding in a warning.
func summarize(diags diag.Diagnostics) string {
	var parts []string
	for _, d := range diags.Errors() {
		parts = append(parts, d.Summary()+": "+d.Detail())
	}
	return strings.Join(parts, "\n")
}

func (r *aliasResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state aliasResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseAliasID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid alias ID in state", err.Error())
		return
	}
	// On import, email is unknown and a missing alias is an error rather than
	// a removal.
	r.readInto(ctx, id, state.Email.ValueString(), &resp.State, &resp.Diagnostics, true)
}

func (r *aliasResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state aliasResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseAliasID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid alias ID in state", err.Error())
		return
	}

	var upd client.AliasUpdate
	if !plan.MailboxIDs.Equal(state.MailboxIDs) {
		ids, diags := int64sFromSet(ctx, plan.MailboxIDs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		upd.MailboxIDs = ids
	}
	if !plan.Note.Equal(state.Note) {
		upd.Note, upd.ClearNote = plan.Note.ValueStringPointer(), plan.Note.IsNull()
	}
	if !plan.Name.Equal(state.Name) {
		upd.Name, upd.ClearName = plan.Name.ValueStringPointer(), plan.Name.IsNull()
	}
	if err := r.client.UpdateAlias(ctx, id, upd); err != nil {
		resp.Diagnostics.AddError("Failed to update SimpleLogin alias", err.Error())
		return
	}

	if !plan.Enabled.Equal(state.Enabled) {
		// Toggle is not idempotent: compare against the live value, not the
		// (possibly stale) state.
		current, err := r.client.GetAlias(ctx, id, state.Email.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read SimpleLogin alias before changing enabled state", err.Error())
			return
		}
		if diags := r.ensureEnabled(ctx, id, current.Enabled, plan.Enabled.ValueBool()); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			// The PATCH above already succeeded: record what is live now.
			var readDiags diag.Diagnostics
			r.readInto(ctx, id, state.Email.ValueString(), &resp.State, &readDiags, false)
			if !readDiags.HasError() {
				resp.Diagnostics.Append(readDiags...)
			}
			return
		}
	}

	r.readInto(ctx, id, state.Email.ValueString(), &resp.State, &resp.Diagnostics, false)
}

func (r *aliasResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state aliasResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseAliasID(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid alias ID in state", err.Error())
		return
	}
	email := state.Email.ValueString()

	// Make sure we delete the alias we think we are deleting.
	current, err := r.client.GetAlias(ctx, id, email)
	if errors.Is(err, client.ErrNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read SimpleLogin alias before deletion", err.Error())
		return
	}
	if !strings.EqualFold(current.Email, email) {
		resp.Diagnostics.AddError("Refusing to delete SimpleLogin alias",
			fmt.Sprintf("Alias ID %d has address %s, but state expects %s. Not deleting it.", id, current.Email, email))
		return
	}

	if err := r.client.DeleteAlias(ctx, id); err != nil {
		// SimpleLogin answers 403 for an alias that no longer exists, e.g.
		// when it was deleted concurrently.
		if client.IsForbidden(err) {
			if _, gerr := r.client.GetAlias(ctx, id, email); errors.Is(gerr, client.ErrNotFound) {
				return
			}
		}
		resp.Diagnostics.AddError("Failed to delete SimpleLogin alias", err.Error())
	}
}

func (r *aliasResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := parseAliasID(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Import a simplelogin_alias by its numeric SimpleLogin alias ID (shown in the alias URL in the dashboard). %s", err))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ensureEnabled toggles the alias at most once to reach the desired state and
// verifies the result. It never retries the toggle.
func (r *aliasResource) ensureEnabled(ctx context.Context, id int64, current, desired bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if current == desired {
		return diags
	}
	got, err := r.client.ToggleAlias(ctx, id)
	if err != nil {
		diags.AddError("Failed to change SimpleLogin alias enabled state",
			fmt.Sprintf("%s\n\nThe toggle request is not retried automatically because it is not idempotent. Run a refresh to see the current state.", err))
		return diags
	}
	if got != desired {
		diags.AddError("SimpleLogin alias enabled state did not change as expected",
			fmt.Sprintf("Wanted enabled=%t, but SimpleLogin reports enabled=%t after toggling alias %d. "+
				"It was probably changed concurrently; run a refresh and apply again.", desired, got, id))
	}
	return diags
}

type stateSetter interface {
	Set(ctx context.Context, val any) diag.Diagnostics
	RemoveResource(ctx context.Context)
}

// readInto reads the alias and writes it to state. If allowRemove is set and
// the alias is confirmed gone, it is removed from state.
func (r *aliasResource) readInto(ctx context.Context, id int64, knownEmail string, state stateSetter, diags *diag.Diagnostics, allowRemove bool) {
	a, err := r.client.GetAlias(ctx, id, knownEmail)
	if errors.Is(err, client.ErrNotFound) && allowRemove {
		state.RemoveResource(ctx)
		return
	}
	if err != nil {
		diags.AddError("Failed to read SimpleLogin alias", err.Error())
		return
	}
	diags.Append(state.Set(ctx, aliasModelFromAPI(a))...)
}

func aliasModelFromAPI(a *client.Alias) aliasResourceModel {
	return aliasResourceModel{
		ID:         types.StringValue(strconv.FormatInt(a.ID, 10)),
		Email:      types.StringValue(a.Email),
		MailboxIDs: int64Set(a.MailboxIDs()),
		Note:       nullableString(a.Note),
		Name:       nullableString(a.Name),
		Enabled:    types.BoolValue(a.Enabled),
	}
}
