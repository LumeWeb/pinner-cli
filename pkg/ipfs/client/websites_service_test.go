package ipfsclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClientWithResponses is a mock implementation of ClientWithResponses for testing
type mockClientWithResponses struct {
	getApiWebsitesFunc             func(ctx context.Context) (*GetApiWebsitesResponse, error)
	postApiWebsitesFunc            func(ctx context.Context, body PostApiWebsitesJSONRequestBody) (*PostApiWebsitesResponse, error)
	getApiWebsitesIdFunc           func(ctx context.Context, id string) (*GetApiWebsitesIdResponse, error)
	putApiWebsitesIdFunc           func(ctx context.Context, id string, body PutApiWebsitesIdJSONRequestBody) (*PutApiWebsitesIdResponse, error)
	deleteApiWebsitesIdFunc        func(ctx context.Context, id string) (*DeleteApiWebsitesIdResponse, error)
	postApiWebsitesIdValidateFunc  func(ctx context.Context, id string) (*PostApiWebsitesIdValidateResponse, error)
	getApiWebsitesDomainSslStatusFunc func(ctx context.Context, domain string) (*GetApiWebsitesDomainSslStatusResponse, error)
}

func (m *mockClientWithResponses) GetApiWebsitesWithResponse(ctx context.Context, reqEditors ...RequestEditorFn) (*GetApiWebsitesResponse, error) {
	if m.getApiWebsitesFunc != nil {
		return m.getApiWebsitesFunc(ctx)
	}
	return nil, nil
}

func (m *mockClientWithResponses) PostApiWebsitesWithResponse(ctx context.Context, body PostApiWebsitesJSONRequestBody, reqEditors ...RequestEditorFn) (*PostApiWebsitesResponse, error) {
	if m.postApiWebsitesFunc != nil {
		return m.postApiWebsitesFunc(ctx, body)
	}
	return nil, nil
}

func (m *mockClientWithResponses) GetApiWebsitesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*GetApiWebsitesIdResponse, error) {
	if m.getApiWebsitesIdFunc != nil {
		return m.getApiWebsitesIdFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockClientWithResponses) PutApiWebsitesIdWithResponse(ctx context.Context, id string, body PutApiWebsitesIdJSONRequestBody, reqEditors ...RequestEditorFn) (*PutApiWebsitesIdResponse, error) {
	if m.putApiWebsitesIdFunc != nil {
		return m.putApiWebsitesIdFunc(ctx, id, body)
	}
	return nil, nil
}

func (m *mockClientWithResponses) DeleteApiWebsitesIdWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*DeleteApiWebsitesIdResponse, error) {
	if m.deleteApiWebsitesIdFunc != nil {
		return m.deleteApiWebsitesIdFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockClientWithResponses) PostApiWebsitesIdValidateWithResponse(ctx context.Context, id string, reqEditors ...RequestEditorFn) (*PostApiWebsitesIdValidateResponse, error) {
	if m.postApiWebsitesIdValidateFunc != nil {
		return m.postApiWebsitesIdValidateFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockClientWithResponses) GetApiWebsitesDomainSslStatusWithResponse(ctx context.Context, domain string, reqEditors ...RequestEditorFn) (*GetApiWebsitesDomainSslStatusResponse, error) {
	if m.getApiWebsitesDomainSslStatusFunc != nil {
		return m.getApiWebsitesDomainSslStatusFunc(ctx, domain)
	}
	return nil, nil
}

func TestNewWebsitesService(t *testing.T) {
	mockClient := &mockClientWithResponses{}
	service := NewWebsitesService(mockClient)

	assert.NotNil(t, service)
}

func TestWebsitesService_List_Success(t *testing.T) {
	now := time.Now()
	expectedItem := WebsiteItem{
		Id:                  1,
		Domain:              "example.com",
		Status:              "active",
		TargetHash:          "QmXxx",
		TargetType:          "ipfs",
		ValidationToken:     "token123",
		Created:             now,
		Updated:             now,
		Expired:             false,
	}

	mockClient := &mockClientWithResponses{
		getApiWebsitesFunc: func(ctx context.Context) (*GetApiWebsitesResponse, error) {
			return &GetApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200: &WebsiteItemResponse{
					Data:  expectedItem,
					Total: 1,
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.List(context.Background())

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, expectedItem.Id, result[0].Id)
	assert.Equal(t, expectedItem.Domain, result[0].Domain)
	assert.Equal(t, expectedItem.Status, result[0].Status)
}

func TestWebsitesService_List_RetryOn500(t *testing.T) {
	callCount := 0
	expectedItem := WebsiteItem{
		Id:         1,
		Domain:     "example.com",
		Status:     "active",
		TargetHash: "QmXxx",
		TargetType: "ipfs",
	}

	mockClient := &mockClientWithResponses{
		getApiWebsitesFunc: func(ctx context.Context) (*GetApiWebsitesResponse, error) {
			callCount++
			if callCount == 1 {
				return &GetApiWebsitesResponse{
					Body:         []byte(`{}`),
					HTTPResponse: &http.Response{StatusCode: http.StatusInternalServerError},
					JSON500: &ErrorResponse{
						Error: "Internal server error",
					},
				}, nil
			}
			return &GetApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200: &WebsiteItemResponse{
					Data:  expectedItem,
					Total: 1,
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.List(context.Background())

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, 2, callCount)
}

func TestWebsitesService_List_NoRetryOn400(t *testing.T) {
	callCount := 0

	mockClient := &mockClientWithResponses{
		getApiWebsitesFunc: func(ctx context.Context) (*GetApiWebsitesResponse, error) {
			callCount++
			return &GetApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusBadRequest},
				JSON400: &ErrorResponse{
					Error: "Bad request",
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.List(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error (400)")
	assert.Nil(t, result)
	assert.Equal(t, 1, callCount)
}

func TestWebsitesService_List_NilResponseBody(t *testing.T) {
	mockClient := &mockClientWithResponses{
		getApiWebsitesFunc: func(ctx context.Context) (*GetApiWebsitesResponse, error) {
			return &GetApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      nil,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.List(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response body")
	assert.Nil(t, result)
}

func TestWebsitesService_Create_Success(t *testing.T) {
	now := time.Now()
	expectedResponse := WebsiteResponse{
		Id:                  1,
		Domain:              "example.com",
		Status:              "active",
		TargetHash:          "QmXxx",
		TargetType:          "ipfs",
		ValidationToken:     "token123",
		Created:             now,
		Updated:             now,
		Expired:             false,
	}

	mockClient := &mockClientWithResponses{
		postApiWebsitesFunc: func(ctx context.Context, body PostApiWebsitesJSONRequestBody) (*PostApiWebsitesResponse, error) {
			return &PostApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
				JSON201:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Create(context.Background(), "example.com", "QmXxx", "ipfs")

	require.NoError(t, err)
	assert.Equal(t, expectedResponse.Id, result.Id)
	assert.Equal(t, expectedResponse.Domain, result.Domain)
	assert.Equal(t, expectedResponse.Status, result.Status)
}

func TestWebsitesService_Create_HTTP200IsError(t *testing.T) {
	mockClient := &mockClientWithResponses{
		postApiWebsitesFunc: func(ctx context.Context, body PostApiWebsitesJSONRequestBody) (*PostApiWebsitesResponse, error) {
			return &PostApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200: &ErrorResponse{
					Error: "Target is broken",
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Create(context.Background(), "example.com", "QmXxx", "ipfs")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Target is broken")
	assert.Nil(t, result)
}

func TestWebsitesService_Create_RetryOn500(t *testing.T) {
	callCount := 0
	expectedResponse := WebsiteResponse{
		Id:         1,
		Domain:     "example.com",
		Status:     "active",
		TargetHash: "QmXxx",
		TargetType: "ipfs",
	}

	mockClient := &mockClientWithResponses{
		postApiWebsitesFunc: func(ctx context.Context, body PostApiWebsitesJSONRequestBody) (*PostApiWebsitesResponse, error) {
			callCount++
			if callCount == 1 {
				return &PostApiWebsitesResponse{
					Body:         []byte(`{}`),
					HTTPResponse: &http.Response{StatusCode: http.StatusInternalServerError},
					JSON500: &ErrorResponse{
						Error: "Internal server error",
					},
				}, nil
			}
			return &PostApiWebsitesResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
				JSON201:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Create(context.Background(), "example.com", "QmXxx", "ipfs")

	require.NoError(t, err)
	assert.Equal(t, expectedResponse.Id, result.Id)
	assert.Equal(t, 2, callCount)
}

func TestWebsitesService_Get_Success(t *testing.T) {
	now := time.Now()
	expectedResponse := WebsiteResponse{
		Id:                  1,
		Domain:              "example.com",
		Status:              "active",
		TargetHash:          "QmXxx",
		TargetType:          "ipfs",
		ValidationToken:     "token123",
		Created:             now,
		Updated:             now,
		Expired:             false,
	}

	mockClient := &mockClientWithResponses{
		getApiWebsitesIdFunc: func(ctx context.Context, id string) (*GetApiWebsitesIdResponse, error) {
			assert.Equal(t, "1", id)
			return &GetApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Get(context.Background(), "1")

	require.NoError(t, err)
	assert.Equal(t, expectedResponse.Id, result.Id)
	assert.Equal(t, expectedResponse.Domain, result.Domain)
}

func TestWebsitesService_Get_NotFound(t *testing.T) {
	mockClient := &mockClientWithResponses{
		getApiWebsitesIdFunc: func(ctx context.Context, id string) (*GetApiWebsitesIdResponse, error) {
			return &GetApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
				JSON404: &ErrorResponse{
					Error: "Website not found",
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Get(context.Background(), "999")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Website not found")
	assert.Nil(t, result)
}

func TestWebsitesService_Update_Success(t *testing.T) {
	now := time.Now()
	expectedResponse := WebsiteResponse{
		Id:                  1,
		Domain:              "updated.com",
		Status:              "active",
		TargetHash:          "QmYyy",
		TargetType:          "ipns",
		ValidationToken:     "token456",
		Created:             now,
		Updated:             now,
		Expired:             false,
	}

	mockClient := &mockClientWithResponses{
		putApiWebsitesIdFunc: func(ctx context.Context, id string, body PutApiWebsitesIdJSONRequestBody) (*PutApiWebsitesIdResponse, error) {
			assert.Equal(t, "1", id)
			return &PutApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Update(context.Background(), "1", "updated.com", "QmYyy", "ipns")

	require.NoError(t, err)
	assert.Equal(t, expectedResponse.Id, result.Id)
	assert.Equal(t, expectedResponse.Domain, result.Domain)
	assert.Equal(t, expectedResponse.TargetHash, result.TargetHash)
}

func TestWebsitesService_Update_Forbidden(t *testing.T) {
	mockClient := &mockClientWithResponses{
		putApiWebsitesIdFunc: func(ctx context.Context, id string, body PutApiWebsitesIdJSONRequestBody) (*PutApiWebsitesIdResponse, error) {
			return &PutApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusForbidden},
				JSON403: &ErrorResponse{
					Error: "Access denied",
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Update(context.Background(), "1", "updated.com", "QmYyy", "ipns")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Access denied")
	assert.Nil(t, result)
}

func TestWebsitesService_Delete_Success(t *testing.T) {
	mockClient := &mockClientWithResponses{
		deleteApiWebsitesIdFunc: func(ctx context.Context, id string) (*DeleteApiWebsitesIdResponse, error) {
			assert.Equal(t, "1", id)
			return &DeleteApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	err := service.Delete(context.Background(), "1")

	require.NoError(t, err)
}

func TestWebsitesService_Delete_Unauthorized(t *testing.T) {
	mockClient := &mockClientWithResponses{
		deleteApiWebsitesIdFunc: func(ctx context.Context, id string) (*DeleteApiWebsitesIdResponse, error) {
			return &DeleteApiWebsitesIdResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusUnauthorized},
				JSON401: &ErrorResponse{
					Error: "Unauthorized",
				},
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	err := service.Delete(context.Background(), "1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unauthorized")
}

func TestWebsitesService_Validate_Success(t *testing.T) {
	expectedResponse := WebsiteValidateResponse{
		Id:      1,
		Domain:  "example.com",
		Valid:   true,
		Message: "DNS configuration is valid",
	}

	mockClient := &mockClientWithResponses{
		postApiWebsitesIdValidateFunc: func(ctx context.Context, id string) (*PostApiWebsitesIdValidateResponse, error) {
			assert.Equal(t, "1", id)
			return &PostApiWebsitesIdValidateResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Validate(context.Background(), "1")

	require.NoError(t, err)
	assert.Equal(t, expectedResponse.Id, result.Id)
	assert.Equal(t, expectedResponse.Domain, result.Domain)
	assert.Equal(t, expectedResponse.Valid, result.Valid)
	assert.Equal(t, expectedResponse.Message, result.Message)
}

func TestWebsitesService_Validate_Invalid(t *testing.T) {
	expectedResponse := WebsiteValidateResponse{
		Id:      1,
		Domain:  "example.com",
		Valid:   false,
		Message: "DNS record not found",
	}

	mockClient := &mockClientWithResponses{
		postApiWebsitesIdValidateFunc: func(ctx context.Context, id string) (*PostApiWebsitesIdValidateResponse, error) {
			return &PostApiWebsitesIdValidateResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Validate(context.Background(), "1")

	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Equal(t, "DNS record not found", result.Message)
}

func TestWebsitesService_Validate_RetryOn500(t *testing.T) {
	callCount := 0
	expectedResponse := WebsiteValidateResponse{
		Id:      1,
		Domain:  "example.com",
		Valid:   true,
		Message: "Valid",
	}

	mockClient := &mockClientWithResponses{
		postApiWebsitesIdValidateFunc: func(ctx context.Context, id string) (*PostApiWebsitesIdValidateResponse, error) {
			callCount++
			if callCount == 1 {
				return &PostApiWebsitesIdValidateResponse{
					Body:         []byte(`{}`),
					HTTPResponse: &http.Response{StatusCode: http.StatusInternalServerError},
					JSON500: &ErrorResponse{
						Error: "Internal server error",
					},
				}, nil
			}
			return &PostApiWebsitesIdValidateResponse{
				Body:         []byte(`{}`),
				HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				JSON200:      &expectedResponse,
			}, nil
		},
	}

	service := NewWebsitesService(mockClient)
	result, err := service.Validate(context.Background(), "1")

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.True(t, result.Valid)
}

func TestFormatErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errResp    *ErrorResponse
		want       string
	}{
		{
			name:       "with error message",
			statusCode: 400,
			errResp:    &ErrorResponse{Error: "Bad request"},
			want:       "API error (400): Bad request",
		},
		{
			name:       "without error message",
			statusCode: 500,
			errResp:    &ErrorResponse{},
			want:       "API error (500)",
		},
		{
			name:       "nil error response",
			statusCode: 404,
			errResp:    nil,
			want:       "API error (404)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatErrorResponse(tt.statusCode, tt.errResp)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}
