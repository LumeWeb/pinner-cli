package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

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

	args := cmd.Args()
	if args.Len() < 2 {
		return fmt.Errorf("usage: pinner websites domains add <website-id-or-domain> <domain>")
	}

	websiteID, err := resolveWebsiteID(ctx, websitesService, args.First())
	if err != nil {
		return err
	}

	domain := args.Get(1)
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

	args := cmd.Args()
	if args.Len() < 2 {
		return fmt.Errorf("usage: pinner websites domains rm <website-id-or-domain> <domain>")
	}

	websiteID, err := resolveWebsiteID(ctx, websitesService, args.First())
	if err != nil {
		return err
	}

	domainArg := args.Get(1)
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

	args := cmd.Args()
	if args.Len() < 2 {
		return fmt.Errorf("usage: pinner websites domains verify <website-id-or-domain> <domain>")
	}

	websiteID, err := resolveWebsiteID(ctx, websitesService, args.First())
	if err != nil {
		return err
	}

	domainArg := args.Get(1)
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
