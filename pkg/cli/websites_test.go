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
)

// mockWebsitesServiceForCLI is a mock implementation of the CLI WebsitesService interface for testing
type mockWebsitesServiceForCLI struct {
	listFunc         func(ctx context.Context) ([]ipfs.WebsiteItem, error)
	createFunc       func(ctx context.Context, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	getFunc          func(ctx context.Context, id string) (*ipfs.WebsiteItem, error)
	updateFunc       func(ctx context.Context, id, domain, cid, targetType string) (*ipfs.WebsiteItem, error)
	deleteFunc       func(ctx context.Context, id string) error
	validateFunc     func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error)
	getSSLStatusFunc func(ctx context.Context, domain string) (*ipfs.WebsiteResponse, error)
	getConfigFunc    func(ctx context.Context) (*ipfs.WebsiteConfigResponse, error)
}

func (m *mockWebsitesServiceForCLI) RequireAuthenticated() error {
	return nil
}

func (m *mockWebsitesServiceForCLI) List(ctx context.Context) ([]ipfs.WebsiteItem, error) {
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
		return m.createFunc(ctx, req.Domain, req.TargetHash, req.TargetType)
	}
	return &ipfs.WebsiteItem{
		Id:         1,
		Domain:     req.Domain,
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

func (m *mockWebsitesServiceForCLI) UpdateWithOptions(ctx context.Context, id string, req ipfs.WebsiteRequest) (*ipfs.WebsiteItem, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, req.Domain, req.TargetHash, req.TargetType)
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
			output := NewOutputFormatter(false, false, false, false)

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

	websites, err := websitesService.List(ctx)
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
			validation = website.ValidationToken
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
			cid: "QmXxx",
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
			cid:  "QmXxx",
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
			cid: "QmXxx",
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
			cid: "QmXxx",
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := &mockWebsitesCreateCommand{
				domain:     tt.domain,
				cid:        tt.cid,
				targetType: tt.targetType,
			}

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
			cid: "QmXxx",
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

			cmd := &mockWebsitesCreateCommand{
				domain:     tt.domain,
				cid:        tt.cid,
				targetType: tt.targetType,
			}

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
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	id := args.First()

	website, err := websitesService.Get(ctx, id)
	if err != nil {
		return err
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
		{"Validation Token", website.ValidationToken},
	}

	if website.ValidationExpiresAt != nil {
		fields = append(fields, Field{"Token Expires", website.ValidationExpiresAt.Format("2006-01-02 15:04:05")})
	}

	if website.DnsZoneId != nil {
		fields = append(fields, Field{"DNS Zone ID", fmt.Sprintf("%d", *website.DnsZoneId)})
	}

	fields = append(fields, Field{"Created", website.Created.Format("2006-01-02 15:04:05")})

	output.PrintFields(FieldGroup{Fields: fields})

	return nil
}

func TestWebsitesGet(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*mockWebsitesServiceForCLI)
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
			cmd:     &mockWebsitesGetCommand{id: "2"},
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         &mockWebsitesGetCommand{id: ""},
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
			cmd:         &mockWebsitesGetCommand{id: "1"},
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

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
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
		cmd         *mockWebsitesUpdateCommand
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update with all parameters",
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "new-example.com",
				cid: "QmNewHash",
				targetType: "ipfs",
			},
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
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "new-domain.com",
				cid: "",
				targetType: "",
			},
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
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "",
				cid: "QmNewHash",
				targetType: "",
			},
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
			name: "missing website ID",
			cmd: &mockWebsitesUpdateCommand{
				id:         "",
				domain:     "new-example.com",
				cid: "QmNewHash",
				targetType: "ipfs",
			},
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "missing update fields (all empty)",
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "",
				cid: "",
				targetType: "",
			},
			setupMocks:  func(svc *mockWebsitesServiceForCLI) {},
			wantErr:     true,
			errContains: "at least one field must be provided for update",
		},
		{
			name: "service error",
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "new-example.com",
				cid: "QmNewHash",
				targetType: "ipfs",
			},
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
			output := NewOutputFormatter(false, false, false, false)

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
		cmd         *mockWebsitesUpdateCommand
		setupMocks  func(*mockWebsitesServiceForCLI)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful update with JSON output",
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "new-example.com",
				cid: "QmNewHash",
				targetType: "ipfs",
			},
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
			cmd: &mockWebsitesUpdateCommand{
				id:         "1",
				domain:     "new-domain.com",
				cid: "",
				targetType: "",
			},
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

// mockWebsitesCreateCommand is a mock implementation of commandGetter for testing.
type mockWebsitesCreateCommand struct {
	domain string
	cid    string
	targetType string
}

func (m *mockWebsitesCreateCommand) String(name string) string {
	switch name {
	case FlagDomain:
		return m.domain
	case FlagCID:
		return m.cid
	case FlagTargetType:
		return m.targetType
	default:
		return ""
	}
}

// mockWebsitesGetCommand is a mock implementation of commandGetter for testing.
type mockWebsitesGetCommand struct {
	id string
}

func (m *mockWebsitesGetCommand) String(name string) string {
	return ""
}

func (m *mockWebsitesGetCommand) Args() cli.Args {
	if m.id == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.id}}
}

// mockWebsitesUpdateCommand is a mock implementation of commandGetter for testing.
type mockWebsitesUpdateCommand struct {
	id         string
	domain     string
	cid        string
	targetType string
}

func (m *mockWebsitesUpdateCommand) String(name string) string {
	switch name {
	case FlagDomain:
		return m.domain
	case FlagCID:
		return m.cid
	case FlagTargetType:
		return m.targetType
	default:
		return ""
	}
}

func (m *mockWebsitesUpdateCommand) Args() cli.Args {
	if m.id == "" {
		return &mockArgs{}
	}
	return &mockArgs{[]string{m.id}}
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
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	id := args.First()

	domain := cmd.String(FlagDomain)
	cid := cmd.String(FlagCID)
	targetType := cmd.String(FlagTargetType)

	if domain == "" && cid == "" && targetType == "" {
		return fmt.Errorf("at least one field must be provided for update (domain, cid, or target-type)")
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
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
			wantErr: false,
		},
		{
			name: "successful delete website with string ID",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.deleteFunc = func(ctx context.Context, id string) error {
					return nil
				}
			},
			cmd:     &mockWebsitesGetCommand{id: "2"},
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         &mockWebsitesGetCommand{id: ""},
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
			cmd:         &mockWebsitesGetCommand{id: "1"},
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

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
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	id := args.First()

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
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
			wantErr: false,
		},
		{
			name:        "missing website ID",
			cmd:         &mockWebsitesGetCommand{id: ""},
			wantErr:     true,
			errContains: "website ID or domain is required",
		},
		{
			name: "service error",
			setupMocks: func(svc *mockWebsitesServiceForCLI) {
				svc.validateFunc = func(ctx context.Context, id string) (*ipfs.WebsiteValidateResponse, error) {
					return nil, errors.New("website not found")
				}
			},
			cmd:         &mockWebsitesGetCommand{id: "1"},
			wantErr:     true,
			errContains: "website not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockWebsitesServiceForCLI{}
			output := NewOutputFormatter(false, false, false, false)

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
		cmd         *mockWebsitesGetCommand
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
			cmd:     &mockWebsitesGetCommand{id: "1"},
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
func websitesValidateWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, websitesService WebsitesService) error {
	if err := websitesService.RequireAuthenticated(); err != nil {
		return err
	}

	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("website ID or domain is required")
	}

	id := args.First()

	validationResult, err := websitesService.Validate(ctx, id)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(validationResult)
	}

	output.Printf("Website Validation Result")

	statusIcon := "⏳"
	if validationResult.Valid {
		statusIcon = "✅"
	}

	output.PrintFields(FieldGroup{
		Fields: []Field{
			{"Domain", validationResult.Domain},
			{"ID", fmt.Sprintf("%d", validationResult.Id)},
			{"Valid", fmt.Sprintf("%s %t", statusIcon, validationResult.Valid)},
			{"Message", validationResult.Message},
		},
	})

	return nil
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
			output := NewOutputFormatter(false, false, false, false)

			if tt.setupMocks != nil {
				tt.setupMocks(mockSvc)
			}

			cmd := &mockSSLStatusCommand{domain: "example.com"}
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

// mockSSLStatusCommand is a mock command for testing SSL status
type mockSSLStatusCommand struct {
	domain string
}

func (m *mockSSLStatusCommand) Args() cli.Args {
	return &mockArgs{[]string{m.domain}}
}

// websitesSSLStatusWithService is a test helper that allows injecting a mock WebsitesService
func websitesSSLStatusWithService(ctx context.Context, cmd interface{ Args() cli.Args }, output Output, websitesService WebsitesService) error {
	args := cmd.Args()
	if args.Len() == 0 {
		return fmt.Errorf("domain is required")
	}

	domain := args.First()

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
			output := NewOutputFormatter(false, false, false, false)

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
