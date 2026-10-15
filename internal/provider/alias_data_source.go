// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/qnimbus/terraform-provider-simplelogin/internal/client"
)

var _ datasource.DataSourceWithConfigure = &aliasDataSource{}

// NewAliasDataSource returns the simplelogin_alias data source.
func NewAliasDataSource() datasource.DataSource { return &aliasDataSource{} }

type aliasDataSource struct {
	client *client.Client
}

type aliasDataSourceModel struct {
	ID                types.String `tfsdk:"id"`
	Email             types.String `tfsdk:"email"`
	MailboxIDs        types.Set    `tfsdk:"mailbox_ids"`
	Note              types.String `tfsdk:"note"`
	Name              types.String `tfsdk:"name"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	CreationTimestamp types.Int64  `tfsdk:"creation_timestamp"`
}

func (d *aliasDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alias"
}

func (d *aliasDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads an existing SimpleLogin alias by its ID.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Required: true, MarkdownDescription: "SimpleLogin alias ID."},
			"email":              schema.StringAttribute{Computed: true, MarkdownDescription: "Alias address."},
			"mailbox_ids":        schema.SetAttribute{Computed: true, ElementType: types.Int64Type, MarkdownDescription: "IDs of the mailboxes the alias forwards to."},
			"note":               schema.StringAttribute{Computed: true, MarkdownDescription: "Alias note, or null."},
			"name":               schema.StringAttribute{Computed: true, MarkdownDescription: "Alias display name, or null."},
			"enabled":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the alias forwards mail."},
			"creation_timestamp": schema.Int64Attribute{Computed: true, MarkdownDescription: "Creation time as a Unix timestamp."},
		},
	}
}

func (d *aliasDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, err := clientFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	d.client = c
}

func (d *aliasDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg aliasDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := parseAliasID(cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("id"), "Invalid alias ID", err.Error())
		return
	}
	a, err := d.client.GetAlias(ctx, id, "")
	if err != nil {
		resp.Diagnostics.AddError("Failed to read SimpleLogin alias", err.Error())
		return
	}
	m := aliasModelFromAPI(a, types.StringNull())
	resp.Diagnostics.Append(resp.State.Set(ctx, aliasDataSourceModel{
		ID:                m.ID,
		Email:             m.Email,
		MailboxIDs:        m.MailboxIDs,
		Note:              m.Note,
		Name:              m.Name,
		Enabled:           m.Enabled,
		CreationTimestamp: types.Int64Value(a.CreationTimestamp),
	})...)
}
