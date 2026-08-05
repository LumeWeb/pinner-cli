package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestWebsitesDomainsList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list domains",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName},
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com"),
			wantErr: false,
		},
		{
			name: "list domains empty",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com"),
			wantErr: false,
		},
		{
			name: "list domains service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return nil, errors.New("failed to list domains")
				}
			},
			cmd:         newMockCommand().withArgs("example.com"),
			wantErr:     true,
			errContains: "failed to list domains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsListWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebsitesDomainsListJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "example.com"},
		}, nil
	}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName},
			{Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("example.com")

	err := websitesDomainsListWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsAdd(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful add domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann"),
			wantErr: false,
		},
		{
			name: "successful add domain with --website flag",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withString(FlagWebsite, "example.com").withArgs("mydomain.com").withString("namespace", "icann"),
			wantErr: false,
		},
		{
			name: "both --website flag and positional website errors",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return nil, nil
				}
			},
			cmd:         newMockCommand().withString(FlagWebsite, "example.com").withArgs("example.com", "mydomain.com"),
			wantErr:     true,
			errContains: "both as --website flag and positional",
		},
		{
			name: "successful add domain with hns namespace",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "mydomain.hns").withString("namespace", "hns"),
			wantErr: false,
		},
		{
			name: "invalid namespace",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "invalid"),
			wantErr:     true,
			errContains: `invalid namespace "invalid"`,
		},
		{
			name: "missing args",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com"),
			wantErr:     true,
			errContains: "usage: pinner websites domains add",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
					return nil, errors.New("bind failed")
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann"),
			wantErr:     true,
			errContains: "bind failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsAddWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebsitesDomainsAddJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
		return []ipfs.WebsiteItem{
			{Id: 1, Domain: "example.com"},
		}, nil
	}
	mockSvc.BindDomainFn = func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("example.com", "mydomain.com").withString("namespace", "icann")

	err := websitesDomainsAddWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsRm(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful remove domain by numeric ID",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("1", strconv.Itoa(42)),
			wantErr: false,
		},

		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 99, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return errors.New("domain not found")
				}
			},
			cmd:         newMockCommand().withArgs("1", strconv.Itoa(99)),
			wantErr:     true,
			errContains: "domain not found",
		},
		{
			name: "successful remove domain by domain name",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "staging.example.com"),
			wantErr: false,
		},
		{
			name: "successful remove domain by domain name with --website flag",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
				svc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withString(FlagWebsite, "example.com").withArgs("staging.example.com"),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "missing.example.com"),
			wantErr:     true,
			errContains: `domain "missing.example.com" not found for website`,
		},
		{
			name:        "missing args",
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "usage: pinner websites domains rm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsRmWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebsitesDomainsRmJSON(t *testing.T) {
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 42, Domain: "staging.example.com", Namespace: "icann"},
		}, nil
	}
	mockSvc.UnbindDomainFn = func(ctx context.Context, websiteID string, domainID string) error {
		return nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("1", "42")

	err := websitesDomainsRmWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestWebsitesDomainsVerify(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful verify domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				zoneName := "example.com."
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 1, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1", "1"),
			wantErr: false,
		},
		{
			name: "successful verify domain by name",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com", "mydomain.hns"),
			wantErr: false,
		},
		{
			name: "successful verify by name with --website flag",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withString(FlagWebsite, "example.com").withArgs("mydomain.hns"),
			wantErr: false,
		},
		{
			name: "successful verify by name with trailing dots",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", ZoneName: nil,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("example.com.", "mydomain.hns."),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{Id: 1, Domain: "example.com"},
					}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "unknown.example.com"),
			wantErr:     true,
			errContains: `domain "unknown.example.com" not found for website`,
		},
		{
			name:        "missing args",
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "usage: pinner websites domains verify",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, errors.New("verify failed")
				}
			},
			cmd:         newMockCommand().withArgs("1", "42"),
			wantErr:     true,
			errContains: "verify failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsVerifyWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebsitesDomainsVerifyJSON(t *testing.T) {
	zoneName := "example.com."
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.com", Namespace: "icann"},
		}, nil
	}
	mockSvc.VerifyDomainFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", ZoneName: &zoneName,
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("1", "1")

	err := websitesDomainsVerifyWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

// websitesDomainsListWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsListWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return errors.New("website ID or domain is required")
	}

	websiteID, err := resolveWebsiteID(ctx, websitesService, args.First())
	if err != nil {
		return err
	}

	domains, err := websitesService.ListDomains(ctx, websiteID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		if domains == nil {
			domains = []ipfs.DomainResponse{}
		}
		return output.PrintJSON(map[string]any{
			"count":   len(domains),
			"domains": domains,
		})
	}

	if len(domains) == 0 {
		output.Printfln("No domains found for website %s", websiteID)
		return nil
	}

	output.Printfln("Found %d domain(s) for website %s", len(domains), websiteID)

	headers := []string{"ID", "DOMAIN", "NAMESPACE", "STATUS", "ZONE NAME"}
	rows := make([][]string, len(domains))
	for i, d := range domains {
		zoneName := ""
		if d.ZoneName != nil {
			zoneName = *d.ZoneName
		}
		status := ""
		if d.Status != nil {
			status = *d.Status
		}
		rows[i] = []string{
			strconv.Itoa(d.Id),
			d.Domain,
			d.Namespace,
			status,
			zoneName,
		}
	}

	output.PrintTable(headers, rows)
	return nil
}

// websitesDomainsAddWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsAddWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID, domain, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "add")
	if err != nil {
		return err
	}

	namespace := cmd.String("namespace")
	if namespace != "icann" && namespace != "hns" {
		return fmt.Errorf("invalid namespace %q: must be 'icann' or 'hns'", namespace)
	}

	req := ipfs.DomainRequest{
		Domain:    domain,
		Namespace: namespace,
	}

	result, err := websitesService.BindDomain(ctx, websiteID, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain bound successfully")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

// websitesDomainsRmWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsRmWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "rm")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	if err := websitesService.UnbindDomain(ctx, websiteID, domainID); err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"deleted":   true,
			"domain_id": domainID,
		})
	}

	output.Printfln("Domain %s removed from website %s", domainArg, websiteID)
	return nil
}

// websitesDomainsVerifyWithService is a test helper that allows injecting a mock WebsitesService
func websitesDomainsVerifyWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "verify")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.VerifyDomain(ctx, websiteID, domainID)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	output.Printfln("Domain verification triggered")
	zoneName := ""
	if result.ZoneName != nil {
		zoneName = *result.ZoneName
	}
	status := ""
	if result.Status != nil {
		status = *result.Status
	}
	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", strconv.Itoa(result.Id)},
			{"Domain", result.Domain},
			{"Namespace", result.Namespace},
			{"Status", status},
			{"Zone Name", zoneName},
		},
	})

	return nil
}

func TestResolveDomainID(t *testing.T) {
	mkSvc := func(domains []ipfs.DomainResponse) *mockWebsitesServiceForCLI {
		svc := &mockWebsitesServiceForCLI{}
		svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
			return domains, nil
		}
		return svc
	}

	domains := []ipfs.DomainResponse{
		{Id: 1, Domain: "lumeweb", Namespace: "hns"},
		{Id: 2, Domain: "example.com", Namespace: "icann"},
	}

	tests := []struct {
		name      string
		arg       string
		want      string
		wantErr   bool
		errPrefix string
	}{
		{"exact name", "lumeweb", "1", false, ""},
		{"trailing dot arg matches bare stored name", "lumeweb.", "1", false, ""},
		{"case insensitive", "LUMEWEB", "1", false, ""},
		{"case insensitive trailing dot", "LUMEWEB.", "1", false, ""},
		{"icann domain", "example.com.", "2", false, ""},
		{"numeric id", "2", "2", false, ""},
		{"not found", "nonexistent", "", true, "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mkSvc(domains)
			got, err := resolveDomainID(context.Background(), svc, "1", tt.arg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errPrefix)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// websitesDomainsDNSRequirementsWithService is a test helper that allows
// injecting a mock WebsitesService. Mirrors websitesDomainsDNSRequirements.
func websitesDomainsDNSRequirementsWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websiteID, domainArg, err := resolveDomainCommandTarget(ctx, websitesService, cmd, "dns-requirements")
	if err != nil {
		return err
	}

	domainID, err := resolveDomainID(ctx, websitesService, websiteID, domainArg)
	if err != nil {
		return err
	}

	result, err := websitesService.GetDomainDNSRequirements(ctx, websiteID, domainID)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("no DNS requirements returned for domain %s", domainID)
	}

	if output.IsJSON() {
		return output.PrintJSON(result)
	}

	renderDomainDelegation(output, result)
	return nil
}

func TestWebsitesDomainsDNSRequirements(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful dns-requirements without delegation",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1", "2"),
			wantErr: false,
		},
		{
			name: "successful dns-requirements with delegation records",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{
						Id: 2, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
						Delegation: &ipfs.DNSDelegation{
							Mode:         strPtr("delegated"),
							Ds:           strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
							Instructions: strPtr("Publish parent records in your HNS wallet."),
							ParentRecords: &[]ipfs.DNSDelegationRecord{
								{Type: "NS", Value: strPtr("ns1.lumeweb,ns2.lumeweb")},
								{Type: "DS", Value: strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>")},
							},
							AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
								{Type: "NS", Value: strPtr("ns1.lumeweb\nns2.lumeweb")},
								{Type: "TLSA", Value: strPtr("_443._tcp.mydomain. 3 1 1 <sha256>")},
							},
						},
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1", "2"),
			wantErr: false,
		},
		{
			name: "successful by name with --website flag",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return &ipfs.DomainResponse{Id: 2, Domain: "mydomain.hns", Namespace: "hns"}, nil
				}
			},
			cmd:     newMockCommand().withString(FlagWebsite, "example.com").withArgs("mydomain.hns"),
			wantErr: false,
		},
		{
			name: "domain name not found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{{Id: 1, Domain: "example.com"}}, nil
				}
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 2, Domain: "mydomain.hns", Namespace: "hns"},
					}, nil
				}
			},
			cmd:         newMockCommand().withArgs("example.com", "unknown.example.com"),
			wantErr:     true,
			errContains: `domain "unknown.example.com" not found for website`,
		},
		{
			name:        "missing args",
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "usage: pinner websites domains dns-requirements",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, errors.New("fetch failed")
				}
			},
			cmd:         newMockCommand().withArgs("1", "42"),
			wantErr:     true,
			errContains: "fetch failed",
		},
		{
			name: "nil result without error is guarded",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
					return []ipfs.DomainResponse{
						{Id: 42, Domain: "mydomain.com", Namespace: "icann"},
					}, nil
				}
				svc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
					return nil, nil
				}
			},
			cmd:         newMockCommand().withArgs("1", "42"),
			wantErr:     true,
			errContains: "no DNS requirements returned for domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDomainsDNSRequirementsWithService(context.Background(), tt.cmd, output, mockSvc)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebsitesDomainsDNSRequirementsJSON(t *testing.T) {
	mockSvc := &mockWebsitesServiceForCLI{}
	mockSvc.ListDomainsFn = func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
		return []ipfs.DomainResponse{
			{Id: 1, Domain: "mydomain.hns", Namespace: "hns"},
		}, nil
	}
	mockSvc.GetDomainDNSRequirementsFn = func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
		return &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				Ds:   strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
			},
		}, nil
	}

	output := NewOutputFormatter(true, false, false, false)
	cmd := newMockCommand().withArgs("1", "1")

	err := websitesDomainsDNSRequirementsWithService(context.Background(), cmd, output, mockSvc)
	require.NoError(t, err)
}

func TestRenderDomainDelegation(t *testing.T) {
	t.Run("renders no delegation message when nil", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
		})
		// exercises the nil-delegation branch without asserting exact text
	})

	t.Run("renders records with typed helper", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode:         strPtr("delegated"),
				Ds:           strPtr("mydomain. 3600 IN DS 12345 13 2 <digest>"),
				Instructions: strPtr("Publish parent records in your HNS wallet."),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb,ns2.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain. 3 1 1 <sha256>")},
				},
			},
		})
		// exercises the non-nil typed-helper path
	})

	t.Run("inline mode labels authoritative records via synthetic nameservers", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
			},
		})
		// In inline mode the authoritative side is served automatically via
		// synthetic nameserver names — it is not user-configured.
	})

	t.Run("icann driver renders registrar wording and nameservers", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: "icann", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Nameservers:  &[]string{"ns1.example.com", "ns2.example.com"},
				Instructions: strPtr("Configure these NS records at your registrar for mydomain.com"),
			},
		})
		// exercises the icann driver path (registrar wording, nameservers list)
	})

	t.Run("unknown namespace falls back to generic driver", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.eth", Namespace: "ens", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain.eth. 3 1 1 <sha256>")},
				},
			},
		})
		// exercises the generic fallback path for an unrecognized namespace
	})
}
