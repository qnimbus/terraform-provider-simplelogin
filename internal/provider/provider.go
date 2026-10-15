// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/qnimbus/terraform-provider-simplelogin/internal/client"
)

const (
	envAPIKey  = "SIMPLELOGIN_API_KEY"
	envBaseURL = "SIMPLELOGIN_BASE_URL"
)

var _ provider.Provider = &SimpleLoginProvider{}

// SimpleLoginProvider is the simplelogin provider.
type SimpleLoginProvider struct {
	// version is set to the provider version on release, "dev" when built
	// locally and "test" when running tests.
	version string
}

type providerModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

func (p *SimpleLoginProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "simplelogin"
	resp.Version = p.version
}

func (p *SimpleLoginProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [SimpleLogin](https://simplelogin.io) email aliases and their forwarding mailboxes.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "SimpleLogin API key, created under *API Keys* in the SimpleLogin dashboard. " +
					"May also be set with the `" + envAPIKey + "` environment variable, which is the recommended way.",
				Optional:  true,
				Sensitive: true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the SimpleLogin instance, without the `/api` path. Defaults to `" + client.DefaultBaseURL +
					"`. May also be set with the `" + envBaseURL + "` environment variable. Useful for self-hosted instances.",
				Optional: true,
			},
		},
	}
}

func (p *SimpleLoginProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Unknown SimpleLogin API key",
			"The provider cannot be configured with an API key that is only known after apply. Use a static value or the "+envAPIKey+" environment variable.")
	}
	if cfg.BaseURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("base_url"), "Unknown SimpleLogin base URL",
			"The provider cannot be configured with a base URL that is only known after apply.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := os.Getenv(envAPIKey)
	if !cfg.APIKey.IsNull() {
		apiKey = cfg.APIKey.ValueString()
	}
	baseURL := os.Getenv(envBaseURL)
	if !cfg.BaseURL.IsNull() {
		baseURL = cfg.BaseURL.ValueString()
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing SimpleLogin API key",
			"Set the "+envAPIKey+" environment variable or the provider api_key attribute. "+
				"Create an API key in the SimpleLogin dashboard under API Keys.")
		return
	}

	userAgent := fmt.Sprintf("terraform-provider-simplelogin/%s (+https://github.com/qnimbus/terraform-provider-simplelogin) %s", p.version, req.TerraformVersion)
	c, err := client.New(baseURL, apiKey, client.WithUserAgent(userAgent))
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("base_url"), "Invalid SimpleLogin provider configuration", err.Error())
		return
	}
	tflog.Debug(ctx, "configured SimpleLogin client", map[string]any{"base_url": baseURL})

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *SimpleLoginProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAliasResource,
	}
}

func (p *SimpleLoginProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAliasDataSource,
		NewMailboxDataSource,
		NewDomainDataSource,
	}
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &SimpleLoginProvider{version: version}
	}
}

// clientFrom extracts the configured client from ProviderData. It returns nil
// without error before the provider is configured (e.g. during validation).
func clientFrom(data any) (*client.Client, error) {
	if data == nil {
		return nil, nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		return nil, fmt.Errorf("expected *client.Client, got %T; please report this issue to the provider developers", data)
	}
	return c, nil
}
