package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"
	websitecore "go.lumeweb.com/pinner-cli/internal/core/websites"
)

// mockWebsitesServiceForCLI is a mock implementation of the CLI WebsitesService interface for testing
type mockWebsitesServiceForCLI struct {
	listFunc                    func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	createFunc                  func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	getFunc                     func(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	updateFunc                  func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	updateWithOptionsFunc       func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error)
	deleteFunc                  func(ctx context.Context, id string) error
	validateFunc                func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	getSSLStatusFunc            func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
	getConfigFunc               func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
	ListDomainsFn               func(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error)
	BindDomainFn                func(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error)
	UnbindDomainFn              func(ctx context.Context, websiteID string, domainID string) error
	VerifyDomainFn              func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	GetDomainDNSRequirementsFn  func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error)
	RepublishDANEFn             func(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error)
	UpdateDomainFn              func(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error)
	CheckPlatformAvailabilityFn func(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error)
	ListPlatformDomainsFn       func(ctx context.Context) (*ipfs.PlatformDomainListResponse, error)
}

func (m *mockWebsitesServiceForCLI) RequireAuthenticated() error {
	return nil
}

func (m *mockWebsitesServiceForCLI) SetAuthToken(token string) {}

func (m *mockWebsitesServiceForCLI) List(ctx context.Context, opts websitecore.ListOptions) ([]ipfs.WebsiteItem, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []ipfs.WebsiteItem{
		{
			Id:         1,
			Domain:     "example.com",
			TargetHash: "QmXxx",
			Status:     "active",
			Created:    time.Now(),
		},
	}, nil
}

func (m *mockWebsitesServiceForCLI) Create(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, domain, cid, targetType)
	}
	return &ipfs.WebsiteItem{
		Id:         1,
		Domain:     domain,
		TargetHash: cid,
		Status:     "active",
		Created:    time.Now(),
	}, nil
}

func (m *mockWebsitesServiceForCLI) CreateWithOptions(ctx context.Context, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, sOrEmpty(req.Domain), req.TargetHash, req.TargetType)
	}
	return &ipfs.WebsiteItem{
		Id:         1,
		Domain:     sOrEmpty(req.Domain),
		TargetHash: req.TargetHash,
		TargetType: req.TargetType,
		Status:     "active",
		Created:    time.Now(),
	}, nil
}

func (m *mockWebsitesServiceForCLI) Get(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) Update(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, domain, cid, targetType)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
	if m.updateWithOptionsFunc != nil {
		return m.updateWithOptionsFunc(ctx, id, req)
	}
	if m.updateFunc != nil {
		domain := ""
		if req.Domain != nil {
			domain = *req.Domain
		}
		targetHash := ""
		if req.TargetHash != nil {
			targetHash = *req.TargetHash
		}
		targetType := ""
		if req.TargetType != nil {
			targetType = *req.TargetType
		}
		return m.updateFunc(ctx, id, domain, targetHash, targetType)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWebsitesServiceForCLI) Validate(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) GetSSLStatus(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
	if m.getSSLStatusFunc != nil {
		return m.getSSLStatusFunc(ctx, domain)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) GetConfig(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) ListDomains(ctx context.Context, websiteID string) ([]ipfs.DomainResponse, error) {
	if m.ListDomainsFn != nil {
		return m.ListDomainsFn(ctx, websiteID)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) BindDomain(ctx context.Context, websiteID string, req ipfs.DomainRequest) (*ipfs.DomainResponse, error) {
	if m.BindDomainFn != nil {
		return m.BindDomainFn(ctx, websiteID, req)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) UnbindDomain(ctx context.Context, websiteID string, domainID string) error {
	if m.UnbindDomainFn != nil {
		return m.UnbindDomainFn(ctx, websiteID, domainID)
	}
	return nil
}

func (m *mockWebsitesServiceForCLI) VerifyDomain(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if m.VerifyDomainFn != nil {
		return m.VerifyDomainFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) GetDomainDNSRequirements(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainResponse, error) {
	if m.GetDomainDNSRequirementsFn != nil {
		return m.GetDomainDNSRequirementsFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) RepublishDANE(ctx context.Context, websiteID string, domainID string) (*ipfs.DomainDANERepublishResponse, error) {
	if m.RepublishDANEFn != nil {
		return m.RepublishDANEFn(ctx, websiteID, domainID)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) UpdateDomain(ctx context.Context, websiteID string, domainID string, req ipfs.DomainUpdateRequest) (*ipfs.DomainResponse, error) {
	if m.UpdateDomainFn != nil {
		return m.UpdateDomainFn(ctx, websiteID, domainID, req)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) ListPlatformDomains(ctx context.Context) (*ipfs.PlatformDomainListResponse, error) {
	if m.ListPlatformDomainsFn != nil {
		return m.ListPlatformDomainsFn(ctx)
	}
	return nil, nil
}

func (m *mockWebsitesServiceForCLI) CheckPlatformDomainAvailability(ctx context.Context, label string) (*ipfs.PlatformAvailabilityResponse, error) {
	if m.CheckPlatformAvailabilityFn != nil {
		return m.CheckPlatformAvailabilityFn(ctx, label)
	}
	return nil, nil
}

func TestWebsitesList(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list websites",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{
							Id:         1,
							Domain:     "example.com",
							TargetHash: "QmXxx",
							Status:     "active",
							Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						},
						{
							Id:         2,
							Domain:     "test.com",
							TargetHash: "QmYyy",
							Status:     "active",
							Created:    time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
						},
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "no websites found",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return nil, errors.New("failed to list websites")
				}
			},
			wantErr:     true,
			errContains: "failed to list websites",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := &cli.Command{}

			err := websitesListWithService(context.Background(), cmd, output, mockSvc)

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

func TestWebsitesListJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful list websites JSON output",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
					return []ipfs.WebsiteItem{
						{
							Id:         1,
							Domain:     "example.com",
							TargetHash: "QmXxx",
							Status:     "active",
							Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
						},
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := &cli.Command{}

			err := websitesListWithService(context.Background(), cmd, output, mockSvc)

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

// websitesListWithService is a test helper that allows injecting a mock WebsitesService
func websitesListWithService(ctx context.Context, cmd *cli.Command, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	websites, err := websitesService.List(ctx, websitecore.ListOptions{})
	if err != nil {
		return err
	}

	if len(websites) == 0 {
		output.Printf("No websites found")
		return nil
	}

	if output.IsJSON() {
		result := map[string]any{
			"count":    len(websites),
			"websites": websites,
		}
		return output.PrintJSON(result)
	}

	output.Printf("Found %d website(s)", len(websites))

	headers := []string{"ID", "NAME", "CID", "STATUS", "DNS", "GATEWAY", "VALIDATION", "CREATED"}
	rows := make([][]string, len(websites))
	for i, website := range websites {
		validation := "valid"
		if website.Expired {
			validation = "expired"
		} else if website.ValidationToken != "" {
			validation = websitecore.StripValidationPrefix(website.ValidationToken)
		}
		gateway := ""
		if website.GatewayDomain != nil {
			gateway = *website.GatewayDomain
		}
		rows[i] = []string{
			fmt.Sprintf("%d", website.Id),
			website.Domain,
			website.TargetHash,
			website.Status,
			fmt.Sprintf("%t", website.DnsHostingEnabled),
			gateway,
			validation,
			website.Created.Format("2006-01-02 15:04:05"),
		}
	}
	output.PrintTable(headers, rows)

	return nil
}

func TestWebsitesCreate(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		cid         string
		targetType  string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful create website",
			domain:     "example.com",
			cid:        "QmXxx",
			targetType: "ipfs",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.createFunc = func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: cid,
						TargetType: targetType,
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "missing domain",
			domain:      "",
			cid:         "QmXxx",
			targetType:  "ipfs",
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "domain is required",
		},
		{
			name:        "missing cid",
			domain:      "example.com",
			cid:         "",
			targetType:  "ipfs",
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "cid is required",
		},
		{
			name:       "service error",
			domain:     "example.com",
			cid:        "QmXxx",
			targetType: "ipfs",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.createFunc = func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return nil, errors.New("invalid domain")
				}
			},
			wantErr:     true,
			errContains: "invalid domain",
		},
		{
			name:       "default target type",
			domain:     "example.com",
			cid:        "QmXxx",
			targetType: "",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.createFunc = func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: cid,
						TargetType: targetType,
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := newMockCommand().withString(FlagDomain, tt.domain).withString(FlagCID, tt.cid).withString(FlagTargetType, tt.targetType)

			err := websitesCreateWithService(context.Background(), cmd, output, mockSvc)

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

func TestWebsitesCreateJSON(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		cid         string
		targetType  string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful create website JSON output",
			domain:     "example.com",
			cid:        "QmXxx",
			targetType: "ipfs",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.createFunc = func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: cid,
						TargetType: targetType,
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := newMockCommand().withString(FlagDomain, tt.domain).withString(FlagCID, tt.cid).withString(FlagTargetType, tt.targetType)

			err := websitesCreateWithService(context.Background(), cmd, output, mockSvc)

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

// websitesCreateWithService is a test helper that allows injecting a mock WebsitesService
func websitesCreateWithService(ctx context.Context, cmd interface{ String(name string) string }, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	domain := cmd.String(FlagDomain)
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	cid := cmd.String(FlagCID)
	if cid == "" {
		return fmt.Errorf("cid is required")
	}

	targetType := cmd.String(FlagTargetType)
	if targetType == "" {
		targetType = "ipfs"
	}

	createdWebsite, err := websitesService.Create(ctx, domain, cid, targetType)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(createdWebsite)
	}

	output.Printf("Website created successfully")

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", fmt.Sprintf("%d", createdWebsite.Id)},
			{"Domain", createdWebsite.Domain},
			{"CID", createdWebsite.TargetHash},
			{"Target Type", createdWebsite.TargetType},
			{"Status", createdWebsite.Status},
			{"DNS Hosting", fmt.Sprintf("%t", createdWebsite.DnsHostingEnabled)},
			{"Expired", fmt.Sprintf("%t", createdWebsite.Expired)},
			{"Created", createdWebsite.Created.Format("2006-01-02 15:04:05")},
		},
	})

	return nil
}

// websitesGetWithService is a test helper that allows injecting a mock WebsitesService
func websitesGetWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	id := args.First()
	if id == "" {
		return fmt.Errorf("website ID or domain is required")
	}

	website, err := websitesService.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ipfs.ErrGone) || website == nil {
			return err
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printf("Website Details")

	fields := []Field{
		{"ID", fmt.Sprintf("%d", website.Id)},
		{"Domain", website.Domain},
		{"CID", website.TargetHash},
		{"Target Type", website.TargetType},
		{"Status", website.Status},
		{"DNS Hosting", fmt.Sprintf("%t", website.DnsHostingEnabled)},
		{"Expired", fmt.Sprintf("%t", website.Expired)},
		{"Validation Token", websitecore.StripValidationPrefix(website.ValidationToken)},
	}

	if website.ValidationExpiresAt != nil {
		fields = append(fields, Field{"Token Expires", website.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
	}

	if website.ZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *website.ZoneId)})
	}

	fields = append(fields, Field{"Created", website.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

	return nil
}

func TestWebsitesGet(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get website",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     "example.com",
						TargetHash: "QmXxx",
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name: "successful get website with string ID",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         2,
						Domain:     "test.com",
						TargetHash: "QmYyy",
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("2"),
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         newMockCommand().withArgs(""),
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return nil, errors.New("website not found")
				}
			},
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "website not found",
		},
		{
			name: "broken website returns data with ErrGone",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         4,
						Domain:     "get.pinner.xyz",
						TargetHash: "12D3KooWA3wFZ8CSBqfotedCZbBY5T37Aj7Kvj6K8h9MCJVkH5x6",
						TargetType: "ipns",
						Status:     "broken",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, ipfs.ErrGone
				}
			},
			cmd:     newMockCommand().withArgs("4"),
			wantErr: false,
		},
		{
			name: "ErrGone with nil result still errors",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return nil, ipfs.ErrGone
				}
			},
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "gone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesGetWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesGetJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful get website JSON output",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     "example.com",
						TargetHash: "QmXxx",
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name: "broken website JSON output with ErrGone",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         4,
						Domain:     "get.pinner.xyz",
						TargetHash: "12D3KooWA3wFZ8CSBqfotedCZbBY5T37Aj7Kvj6K8h9MCJVkH5x6",
						TargetType: "ipns",
						Status:     "broken",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, ipfs.ErrGone
				}
			},
			cmd:     newMockCommand().withArgs("4"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesGetWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesUpdate(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update with all parameters",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "new-example.com").withString(FlagCID, "QmNewHash").withString(FlagTargetType, "ipfs"),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: cid,
						TargetType: targetType,
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "successful update with domain only",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "new-domain.com").withString(FlagCID, "").withString(FlagTargetType, ""),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: "QmOriginalHash",
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "successful update with cid only",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "").withString(FlagCID, "QmNewHash").withString(FlagTargetType, ""),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     "example.com",
						TargetHash: cid,
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         newMockCommand().withArgs("").withString(FlagRenameTo, "new-example.com").withString(FlagCID, "QmNewHash").withString(FlagTargetType, "ipfs"),
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name:        "missing update fields (all empty)",
			cmd:         newMockCommand().withArgs("1").withString(FlagRenameTo, "").withString(FlagCID, "").withString(FlagTargetType, ""),
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "at least one field must be provided for update",
		},
		{
			name: "service error",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "new-example.com").withString(FlagCID, "QmNewHash").withString(FlagTargetType, "ipfs"),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return nil, errors.New("website not found")
				}
			},
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesUpdateWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesUpdateJSON(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update with JSON output",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "new-example.com").withString(FlagCID, "QmNewHash").withString(FlagTargetType, "ipfs"),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: cid,
						TargetType: targetType,
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "successful update partial parameters with JSON output",
			cmd:  newMockCommand().withArgs("1").withString(FlagRenameTo, "new-domain.com").withString(FlagCID, "").withString(FlagTargetType, ""),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateFunc = func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error) {
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     domain,
						TargetHash: "QmOriginalHash",
						TargetType: "ipfs",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesUpdateWithService(context.Background(), tt.cmd, output, mockSvc)

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

// websitesUpdateWithService is a test helper that allows injecting a mock WebsitesService
func websitesUpdateWithService(ctx context.Context, cmd interface {
	Args() cli.Args
	String(name string) string
}, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	id := args.First()
	if id == "" {
		return fmt.Errorf("website ID or domain is required")
	}

	domain := cmd.String(FlagRenameTo)
	cid := cmd.String(FlagCID)
	targetType := cmd.String(FlagTargetType)

	if domain == "" && cid == "" && targetType == "" {
		return fmt.Errorf("at least one field must be provided for update (rename-to, cid, or target-type)")
	}

	updatedWebsite, err := websitesService.Update(ctx, id, domain, cid, targetType)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	output.Printf("Website updated successfully")

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"ID", fmt.Sprintf("%d", updatedWebsite.Id)},
			{"Domain", updatedWebsite.Domain},
			{"CID", updatedWebsite.TargetHash},
			{"Target Type", updatedWebsite.TargetType},
			{"Status", updatedWebsite.Status},
			{"DNS Hosting", fmt.Sprintf("%t", updatedWebsite.DnsHostingEnabled)},
			{"Expired", fmt.Sprintf("%t", updatedWebsite.Expired)},
			{"Created", updatedWebsite.Created.Format("2006-01-02 15:04:05")},
		},
	})

	return nil
}

func TestWebsitesDelete(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete website",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name: "successful delete website with string ID",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("2"),
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         newMockCommand().withArgs(""),
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.deleteFunc = func(ctx context.Context, id string) error {
					return errors.New("website not found")
				}
			},
			cmd:         newMockCommand().withArgs("1"),
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDeleteWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesDeleteJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful delete website JSON output",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesDeleteWithService(context.Background(), tt.cmd, output, mockSvc)

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

// websitesDeleteWithService is a test helper that allows injecting a mock WebsitesService
func websitesDeleteWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	id := args.First()
	if id == "" {
		return fmt.Errorf("website ID or domain is required")
	}

	if err := websitesService.Delete(ctx, id); err != nil {
		return err
	}

	if output.IsJSON() {
		result := map[string]any{
			"success": true,
			"message": fmt.Sprintf("Website %s deleted successfully", id),
		}
		return output.PrintJSON(result)
	}

	output.Printf("Website %s deleted successfully", id)

	return nil
}

func TestWebsitesValidate(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful validate website (valid)",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return &ipfs.WebsiteValidateResponse{
						Domain:  "example.com",
						Id:      1,
						Message: "Website is valid",
						Valid:   true,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name: "successful validate website (invalid)",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return &ipfs.WebsiteValidateResponse{
						Domain:  "example.com",
						Id:      1,
						Message: "DNS record not found",
						Valid:   false,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         newMockCommand().withArgs(),
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "service error shows instructions",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return nil, errors.New("website not found")
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesValidateWithService(context.Background(), tt.cmd, output, mockSvc)

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

func TestWebsitesValidateJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockCommand
		wantErr     bool
		errContains string
	}{
		{
			name: "successful validate website JSON output (valid)",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return &ipfs.WebsiteValidateResponse{
						Domain:  "example.com",
						Id:      1,
						Message: "Website is valid",
						Valid:   true,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
		{
			name: "successful validate website JSON output (invalid)",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return &ipfs.WebsiteValidateResponse{
						Domain:  "example.com",
						Id:      1,
						Message: "DNS record not found",
						Valid:   false,
					}, nil
				}
			},
			cmd:     newMockCommand().withArgs("1"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesValidateWithService(context.Background(), tt.cmd, output, mockSvc)

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

// websitesValidateWithService is a test helper that allows injecting a mock WebsitesService
func websitesValidateWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	return doWebsitesValidate(ctx, cmd, output, websitesService)
}

func TestWebsitesSSLStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful SSL status check",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
					issuedAt := now.Format(time.RFC3339)
					lastUpdated := now.Add(24 * time.Hour).Format(time.RFC3339)
					var resp ipfs.WebsiteResponse
					// ipfs.WebsiteResponse has unexported fields from oapi-codegen generation, so json.Unmarshal is used to construct it in tests
					if err := json.Unmarshal([]byte(fmt.Sprintf(
						`{"domain":"example.com","ssl":{"status":"active","issued_at":"%s","last_updated_at":"%s"}}`,
						issuedAt, lastUpdated,
					)), &resp); err != nil {
						panic(err)
					}
					return &resp, nil
				}
			},
			wantErr: false,
		},
		{
			name: "SSL status with error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
					var resp ipfs.WebsiteResponse
					// ipfs.WebsiteResponse has unexported fields from oapi-codegen generation, so json.Unmarshal is used to construct it in tests
					if err := json.Unmarshal([]byte(`{"domain":"example.com","ssl":{"status":"error","error":"certificate expired"}}`), &resp); err != nil {
						panic(err)
					}
					return &resp, nil
				}
			},
			wantErr: false,
		},
		{
			name: "SSL status no SSL info",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
					return &ipfs.WebsiteResponse{
						Domain: "example.com",
						Ssl:    nil,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "SSL status API error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getSSLStatusFunc = func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error) {
					return nil, errors.New("API error")
				}
			},
			wantErr:     true,
			errContains: "API error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := newMockCommand().withArgs("example.com")
			err := websitesSSLStatusWithService(context.Background(), cmd, output, mockSvc)

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

// websitesSSLStatusWithService is a test helper that allows injecting a mock WebsitesService
func websitesSSLStatusWithService(ctx context.Context, cmd websitesCommandGetter, output Output, websitesService WebsitesService) error {
	args := cmd.Args()
	domain := args.First()
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	website, err := websitesService.GetSSLStatus(ctx, domain)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(website)
	}

	output.Printf("SSL Status for %s", website.Domain)

	if website.Ssl == nil {
		output.Printf("  No SSL information available")
		return nil
	}

	headers := []string{"Field", "Value"}
	rows := [][]string{
		{"Status", website.Ssl.Status},
		{"Issued At", formatTimePtr(website.Ssl.IssuedAt)},
		{"Last Updated", formatTimePtr(website.Ssl.LastUpdatedAt)},
	}

	if website.Ssl.Error != nil && *website.Ssl.Error != "" {
		rows = append(rows, []string{"Error", *website.Ssl.Error})
	}

	output.PrintTable(headers, rows)

	return nil
}

func TestWebsitesConfig(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful config with gateway domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
					gateway := "gw.pinner.xyz"
					return &ipfs.WebsiteConfigResponse{
						GatewayDomain: &gateway,
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "config with no gateway domain",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
					return &ipfs.WebsiteConfigResponse{}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
					return nil, errors.New("failed to get config")
				}
			},
			wantErr:     true,
			errContains: "failed to get config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesConfigWithService(context.Background(), output, mockSvc)

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

func TestWebsitesConfigJSON(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful config JSON output",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.getConfigFunc = func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error) {
					gateway := "gw.pinner.xyz"
					return &ipfs.WebsiteConfigResponse{
						GatewayDomain: &gateway,
					}, nil
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(true, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesConfigWithService(context.Background(), output, mockSvc)

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

func websitesConfigWithService(ctx context.Context, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	config, err := websitesService.GetConfig(ctx)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(config)
	}

	output.Printf("Website Hosting Configuration")

	if config.GatewayDomain != nil && *config.GatewayDomain != "" {
		output.PrintFields(FieldGroup{
			Fields: []Field{
				{"Gateway Domain", *config.GatewayDomain},
			},
		})
		output.Printf("")
		output.Printf("CNAME record to point your domain to the gateway:")
		output.PrintTable([]string{"NAME", "TYPE", "VALUE"}, [][]string{
			{"<your-domain>", "CNAME", *config.GatewayDomain},
		})
	} else {
		output.Printf("  No gateway domain configured")
	}

	return nil
}

func TestWebsitesEnableIPNS(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *mockCommand
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "enable ipns without cid",
			cmd:  newMockCommand().withArgs("1"),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
					require.NotNil(t, req.TargetType)
					require.Equal(t, "ipns", *req.TargetType)
					require.Nil(t, req.TargetHash)
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     "example.com",
						TargetHash: "12D3KooWTestPeerID",
						TargetType: "ipns",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name: "enable ipns with cid",
			cmd:  newMockCommand().withArgs("1").withString(FlagCID, "QmNewHash").withIsSet(FlagCID, true),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
					require.NotNil(t, req.TargetType)
					require.Equal(t, "ipns", *req.TargetType)
					require.NotNil(t, req.TargetHash)
					require.Equal(t, "QmNewHash", *req.TargetHash)
					return &ipfs.WebsiteItem{
						Id:         1,
						Domain:     "example.com",
						TargetHash: "12D3KooWTestPeerID",
						TargetType: "ipns",
						Status:     "active",
						Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					}, nil
				}
			},
			wantErr: false,
		},
		{
			name:        "missing website id",
			cmd:         newMockCommand().withArgs(""),
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "service error",
			cmd:  newMockCommand().withArgs("1"),
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.updateWithOptionsFunc = func(ctx context.Context, id string, req ipfs.WebsiteUpdateRequest) (*ipfs.WebsiteItem, error) {
					return nil, errors.New("website not found")
				}
			},
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := newTestOutput()

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			err := websitesEnableIPNSWithService(context.Background(), tt.cmd, output, mockSvc)

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

func websitesEnableIPNSWithService(ctx context.Context, cmd interface {
	Args() cli.Args
	IsSet(name string) bool
	String(name string) string
}, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	idArg := args.First()
	if idArg == "" {
		return fmt.Errorf("website ID or domain is required")
	}

	id, err := websitecore.ResolveWebsiteID(ctx, websitesService, idArg)
	if err != nil {
		return err
	}

	ipnsType := "ipns"
	req := ipfs.WebsiteUpdateRequest{
		TargetType: &ipnsType,
	}

	if cmd.IsSet(FlagCID) {
		cid := cmd.String(FlagCID)
		req.TargetHash = &cid
	}

	updatedWebsite, err := websitesService.UpdateWithOptions(ctx, id, req)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(updatedWebsite)
	}

	printWebsiteUpdateResult(output, updatedWebsite, "IPNS enabled for website")

	return nil
}

func TestStripValidationPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"lumeweb-verify=abc123", "abc123"},
		{"key=value", "value"},
		{"no-prefix", "no-prefix"},
		{"=", ""},
		{"a=b=c", "b=c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := websitecore.StripValidationPrefix(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestValidationRecordValue(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		website  *ipfs.WebsiteItem
		expected string
	}{
		{
			name: "derives key from validation record host",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("pinner-verify.example.com"),
				ValidationToken:      "abc123",
			},
			expected: "pinner-verify=abc123",
		},
		{
			name: "custom server key",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("lumeweb-verify.example.com"),
				ValidationToken:      "abc123",
			},
			expected: "lumeweb-verify=abc123",
		},
		{
			name: "token already carries key= prefix is not doubled",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("pinner-verify.example.com"),
				ValidationToken:      "pinner-verify=abc123",
			},
			expected: "pinner-verify=abc123",
		},
		{
			name: "token with foreign prefix is preserved unchanged (no rewrite)",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("pinner-verify.example.com"),
				ValidationToken:      "lumeweb-verify=abc123",
			},
			expected: "pinner-verify=lumeweb-verify=abc123",
		},
		{
			name: "token containing equals as content is not corrupted",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("pinner-verify.example.com"),
				ValidationToken:      "abc=def==",
			},
			expected: "pinner-verify=abc=def==",
		},
		{
			name: "token matching derived key without equals is not stripped",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr("pinner-verify.example.com"),
				ValidationToken:      "pinner-verifyvalue",
			},
			expected: "pinner-verify=pinner-verifyvalue",
		},
		{
			name: "no validation record host falls back to bare token",
			website: &ipfs.WebsiteItem{
				Domain:          "example.com",
				ValidationToken: "abc123",
			},
			expected: "abc123",
		},
		{
			name: "empty validation record host falls back to bare token",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				ValidationRecordHost: strPtr(""),
				ValidationToken:      "abc123",
			},
			expected: "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validationRecordValue(tt.website)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveWebsiteID(t *testing.T) {
	t.Run("numeric ID returned as-is", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		id, err := websitecore.ResolveWebsiteID(context.Background(), mockSvc, "42")
		require.NoError(t, err)
		require.Equal(t, "42", id)
	})

	t.Run("domain resolved via list", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{
				{Id: 7, Domain: "example.com"},
				{Id: 8, Domain: "other.com"},
			}, nil
		}
		id, err := websitecore.ResolveWebsiteID(context.Background(), mockSvc, "example.com")
		require.NoError(t, err)
		require.Equal(t, "7", id)
	})

	t.Run("domain not found", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{}, nil
		}
		_, err := websitecore.ResolveWebsiteID(context.Background(), mockSvc, "missing.com")
		require.Error(t, err)
		require.Contains(t, err.Error(), "website not found for domain")
	})

	t.Run("list service error", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return nil, errors.New("service down")
		}
		_, err := websitecore.ResolveWebsiteID(context.Background(), mockSvc, "example.com")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to look up website by domain")
	})
}

func TestResolveAndGetWebsite(t *testing.T) {
	t.Run("numeric ID fetches directly", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
			return &ipfs.WebsiteItem{Id: 42, Domain: "example.com"}, nil
		}
		website, err := resolveAndGetWebsite(context.Background(), mockSvc, "42")
		require.NoError(t, err)
		require.Equal(t, 42, website.Id)
	})

	t.Run("domain resolves then fetches", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{{Id: 7, Domain: "example.com"}}, nil
		}
		mockSvc.getFunc = func(ctx context.Context, id string) (*ipfs.WebsiteItem, error) {
			return &ipfs.WebsiteItem{Id: 7, Domain: "example.com"}, nil
		}
		website, err := resolveAndGetWebsite(context.Background(), mockSvc, "example.com")
		require.NoError(t, err)
		require.Equal(t, 7, website.Id)
	})

	t.Run("domain not found returns error", func(t *testing.T) {
		mockSvc := &mockWebsitesServiceForCLI{}
		mockSvc.listFunc = func(ctx context.Context) ([]ipfs.WebsiteItem, error) {
			return []ipfs.WebsiteItem{}, nil
		}
		_, err := resolveAndGetWebsite(context.Background(), mockSvc, "missing.com")
		require.Error(t, err)
	})
}

func TestPrintWebsiteUpdateResult(t *testing.T) {
	t.Run("active website without gateway", func(t *testing.T) {
		output := newTestOutput()
		website := &ipfs.WebsiteItem{
			Id:         1,
			Domain:     "example.com",
			TargetHash: "QmXxx",
			TargetType: "ipfs",
			Status:     "active",
			Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		}
		printWebsiteUpdateResult(output, website, "Website updated")
	})

	t.Run("inactive website shows token expired", func(t *testing.T) {
		output := newTestOutput()
		website := &ipfs.WebsiteItem{
			Id:         1,
			Domain:     "example.com",
			TargetHash: "QmXxx",
			TargetType: "ipfs",
			Status:     "pending",
			Expired:    true,
			Created:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		}
		printWebsiteUpdateResult(output, website, "Website updated")
	})

	t.Run("website with gateway domain", func(t *testing.T) {
		output := newTestOutput()
		gateway := "gw.pinner.xyz"
		ipnsKeyID := 5
		website := &ipfs.WebsiteItem{
			Id:            1,
			Domain:        "example.com",
			TargetHash:    "QmXxx",
			TargetType:    "ipfs",
			Status:        "active",
			GatewayDomain: &gateway,
			IpnsKeyId:     &ipnsKeyID,
			Created:       time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		}
		printWebsiteUpdateResult(output, website, "IPNS enabled")
	})
}

func TestShowDNSRecordInstructions(t *testing.T) {
	t.Run("nil website returns early", func(t *testing.T) {
		output := newTestOutput()
		showDNSRecordInstructions(output, nil, nil)
	})

	t.Run("dns hosting enabled", func(t *testing.T) {
		output := newTestOutput()
		website := &ipfs.WebsiteItem{
			Domain:            "example.com",
			DnsHostingEnabled: true,
		}
		showDNSRecordInstructions(output, website, []string{"ns1.pinner.xyz", "ns2.pinner.xyz"})
	})

	t.Run("self-managed dns", func(t *testing.T) {
		output := newTestOutput()
		website := &ipfs.WebsiteItem{
			Domain:          "example.com",
			TargetHash:      "QmXxx",
			TargetType:      "ipfs",
			ValidationToken: "lumeweb-verify=abc123",
		}
		showDNSRecordInstructions(output, website, nil)
	})
}

func TestShowConfigDNSRecords(t *testing.T) {
	t.Run("with gateway domain", func(t *testing.T) {
		output := newTestOutput()
		gateway := "gw.pinner.xyz"
		config := &ipfs.WebsiteConfigResponse{
			GatewayDomain: &gateway,
		}
		showConfigDNSRecords(output, config)
	})

	t.Run("without gateway domain", func(t *testing.T) {
		output := newTestOutput()
		config := &ipfs.WebsiteConfigResponse{}
		showConfigDNSRecords(output, config)
	})
}
