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

var _ datasource.DataSourceWithConfigure = &domainDataSource{}

// NewDomainDataSource returns the simplelogin_domain data source.
func NewDomainDataSource() datasource.DataSource { return &domainDataSource{} }

type domainDataSource struct {
	client *client.Client
}

type domainDataSourceModel struct {
	Domain                 types.String `tfsdk:"domain"`
	ID                     types.Int64  `tfsdk:"id"`
	IsVerified             types.Bool   `tfsdk:"is_verified"`
	CatchAll               types.Bool   `tfsdk:"catch_all"`
	Name                   types.String `tfsdk:"name"`
	RandomPrefixGeneration types.Bool   `tfsdk:"random_prefix_generation"`
	MailboxIDs             types.Set    `tfsdk:"mailbox_ids"`
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a custom domain of the SimpleLogin account by name. Read-only: domains cannot be created or changed with this provider.",
		Attributes: map[string]schema.Attribute{
			"domain":                   schema.StringAttribute{Required: true, MarkdownDescription: "Domain name, e.g. `example.com` (case-insensitive)."},
			"id":                       schema.Int64Attribute{Computed: true, MarkdownDescription: "SimpleLogin custom domain ID."},
			"is_verified":              schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether domain ownership (MX) is verified. Aliases can only be created on verified domains."},
			"catch_all":                schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether catch-all (auto-create aliases on first email) is enabled."},
			"name":                     schema.StringAttribute{Computed: true, MarkdownDescription: "Default display name for aliases on this domain, or null."},
			"random_prefix_generation": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether random prefix generation is enabled for the domain."},
			"mailbox_ids":              schema.SetAttribute{Computed: true, ElementType: types.Int64Type, MarkdownDescription: "Default mailboxes for catch-all aliases on this domain."},
		},
	}
}

func (d *domainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, err := clientFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	d.client = c
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cd, err := d.client.FindCustomDomain(ctx, cfg.Domain.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddAttributeError(path.Root("domain"), "SimpleLogin custom domain not found",
			fmt.Sprintf("No custom domain %q exists in this SimpleLogin account.", cfg.Domain.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to list SimpleLogin custom domains", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, domainDataSourceModel{
		Domain:                 cfg.Domain,
		ID:                     types.Int64Value(cd.ID),
		IsVerified:             types.BoolValue(cd.IsVerified),
		CatchAll:               types.BoolValue(cd.CatchAll),
		Name:                   nullableString(cd.Name),
		RandomPrefixGeneration: types.BoolValue(cd.RandomPrefixGeneration),
		MailboxIDs:             int64Set(client.MailboxRefIDs(cd.Mailboxes)),
	})...)
}
