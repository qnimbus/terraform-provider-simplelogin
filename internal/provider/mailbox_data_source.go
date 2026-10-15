// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/qnimbus/terraform-provider-simplelogin/internal/client"
)

var _ datasource.DataSourceWithConfigure = &mailboxDataSource{}

// NewMailboxDataSource returns the simplelogin_mailbox data source.
func NewMailboxDataSource() datasource.DataSource { return &mailboxDataSource{} }

type mailboxDataSource struct {
	client *client.Client
}

type mailboxDataSourceModel struct {
	ID       types.Int64  `tfsdk:"id"`
	Email    types.String `tfsdk:"email"`
	Verified types.Bool   `tfsdk:"verified"`
	Default  types.Bool   `tfsdk:"default"`
}

func (d *mailboxDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mailbox"
}

func (d *mailboxDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a SimpleLogin mailbox (a forwarding destination) by its email address. " +
			"Use its `id` in `simplelogin_alias.mailbox_ids`.",
		Attributes: map[string]schema.Attribute{
			"email":    schema.StringAttribute{Required: true, MarkdownDescription: "Mailbox email address (case-insensitive)."},
			"id":       schema.Int64Attribute{Computed: true, MarkdownDescription: "SimpleLogin mailbox ID."},
			"verified": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the mailbox is verified. Only verified mailboxes can receive aliases."},
			"default":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this is the account's default mailbox."},
		},
	}
}

func (d *mailboxDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, err := clientFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	d.client = c
}

func (d *mailboxDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg mailboxDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := d.client.FindMailboxByEmail(ctx, cfg.Email.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddAttributeError(path.Root("email"), "SimpleLogin mailbox not found",
			fmt.Sprintf("No mailbox with address %q exists in this SimpleLogin account.", cfg.Email.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to list SimpleLogin mailboxes", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, mailboxDataSourceModel{
		ID:       types.Int64Value(m.ID),
		Email:    cfg.Email,
		Verified: types.BoolValue(m.Verified),
		Default:  types.BoolValue(m.Default),
	})...)
}
