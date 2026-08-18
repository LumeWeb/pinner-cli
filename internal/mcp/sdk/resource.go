package sdk

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yosida95/uritemplate/v3"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// Resource converts a Pinner-owned resource descriptor into an official SDK
// resource.
func Resource(desc model.ResourceDescriptor) *mcp.Resource {
	return &mcp.Resource{
		URI:         desc.URI,
		Name:        desc.Name,
		Title:       desc.Title,
		Description: desc.Description,
		MIMEType:    desc.MIMEType,
	}
}

// resourceHandler adapts a Pinner-owned resource handler to the official SDK
// handler shape. req.Params.URI carries the concrete URI.
func resourceHandler(handler model.ResourceHandler) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		result, err := handler(ctx, model.ResourceRequest{
			URI:       req.Params.URI,
			Arguments: map[string]string{},
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: result.URI, MIMEType: result.MIMEType, Text: result.Text},
			},
		}, nil
	}
}

// resourceTemplateHandler adapts a Pinner-owned resource-template handler. The
// official SDK resolves the template and passes the concrete URI; template
// variables are not populated automatically, so the handler receives the parsed
// URI variables via Arguments.
func resourceTemplateHandler(template string, handler model.ResourceHandler) mcp.ResourceHandler {
	parsed, err := uritemplate.New(template)
	if err != nil {
		panic(fmt.Sprintf("invalid resource template %q: %v", template, err))
	}
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		arguments := map[string]string{}
		matches := parsed.Regexp().FindStringSubmatch(req.Params.URI)
		if matches != nil {
			for i, name := range parsed.Varnames() {
				if i+1 < len(matches) {
					arguments[name] = matches[i+1]
				}
			}
		}
		result, err := handler(ctx, model.ResourceRequest{
			URI:       req.Params.URI,
			Arguments: arguments,
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: result.URI, MIMEType: result.MIMEType, Text: result.Text},
			},
		}, nil
	}
}

// RegisterResources registers static resources and resource templates on an
// official-SDK server.
func RegisterResources(srv *mcp.Server, resources []model.ResourceDescriptor, templates []model.ResourceTemplateDescriptor) error {
	if srv == nil {
		return fmt.Errorf("nil official server")
	}
	for _, r := range resources {
		srv.AddResource(Resource(r), resourceHandler(r.Handler))
	}
	for _, t := range templates {
		srv.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			MIMEType:    t.MIMEType,
		}, resourceTemplateHandler(t.URITemplate, t.Handler))
	}
	return nil
}
