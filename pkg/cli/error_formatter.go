package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	portalsdk "go.lumeweb.com/portal-sdk"
)

// Predefined error types for consistent error handling across the CLI.
// Use errors.Is() to check for these errors instead of string matching.
var (
	// Authentication errors
	ErrNotAuthenticated = errors.New("not authenticated")

	// Input validation errors
	ErrPathRequired   = errors.New("path is required")
	ErrCIDRequired    = errors.New("CID is required")
	ErrInvalidCID     = errors.New("invalid CID")
	ErrNoCIDsProvided = errors.New("no CIDs provided")

	// File system errors
	ErrFileNotFound      = errors.New("file not found")
	ErrDirectoryNotFound = errors.New("directory not found")
	ErrPermissionDenied  = errors.New("permission denied")

	// Pinning errors
	ErrPinNotFound    = errors.New("pin not found")
	ErrPinningFailed  = errors.New("pinning failed")
	ErrStatusCheck    = errors.New("failed to check pin status")
	ErrUnpinAllAborted = errors.New("unpin-all aborted")

	// Upload errors
	ErrUploadFailed      = errors.New("upload failed")
	ErrUploadInterrupted = errors.New("upload interrupted")

	// Download errors
	ErrDownloadFailed = errors.New("download failed")

	// Network errors
	ErrNetworkTimeout   = errors.New("network timeout")
	ErrConnectionFailed = errors.New("connection failed")

	// Configuration errors
	ErrConfigNotFound = errors.New("configuration not found")
	ErrConfigInvalid  = errors.New("configuration invalid")

	// Operation errors
	ErrOperationFailed   = errors.New("operation failed")
	ErrOperationNotFound = errors.New("operation not found")
	ErrServiceUnavailable = errors.New("service unavailable")

	// Benchmark errors
	ErrBenchmarkFailed = errors.New("benchmark failed")
)

// HTTPError represents an HTTP error response with structured detail.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// NewHTTPError creates an HTTPError with the response body trimmed and
// JSON error fields extracted into a readable message.
func NewHTTPError(statusCode int, body string) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Body:       extractErrorMessage(strings.TrimSpace(body)),
	}
}

// extractErrorMessage attempts to extract a human-readable message from an HTTP response body.
// If the body is JSON with an "error" field, it returns that field's value.
// Otherwise, it returns the body as-is.
func extractErrorMessage(body string) string {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil && parsed.Error != "" {
		return parsed.Error
	}
	return body
}

// errorMessages maps error types to user-friendly messages.
var errorMessages = map[error]string{
	// Authentication errors
	ErrNotAuthenticated:       "Not authenticated. Run 'pinner auth login' first",
	portalsdk.ErrUnauthorized: "Authentication expired or invalid. Run 'pinner auth login' to re-authenticate",

	// Input validation errors
	ErrPathRequired:   "Path is required. Usage: pinner upload <path>",
	ErrCIDRequired:    "CID is required",
	ErrInvalidCID:     "Invalid CID format",
	ErrNoCIDsProvided: "No CIDs provided",

	// File system errors
	ErrFileNotFound:      "File not found",
	ErrDirectoryNotFound: "Directory not found",
	ErrPermissionDenied:  "Permission denied",

	// Pinning errors
	ErrPinNotFound:    "Pin not found",
	ErrPinningFailed:  "Pinning operation failed",
	ErrStatusCheck:    "Failed to check pin status",
	ErrUnpinAllAborted: "Unpin-all operation aborted",

	// Upload errors
	ErrUploadFailed:      "Upload failed",
	ErrUploadInterrupted: "Upload interrupted. Try again to resume",

	// Download errors
	ErrDownloadFailed: "Download failed",

	// Network errors
	ErrNetworkTimeout:   "Network timeout. Check your connection",
	ErrConnectionFailed: "Connection failed. Check your internet",

	// Configuration errors
	ErrConfigNotFound: "Configuration not found",
	ErrConfigInvalid:  "Configuration invalid",

	// Operation errors
	ErrOperationFailed:   "Operation failed",
	ErrOperationNotFound: "Operation not found",

	// Context errors
	portalsdk.ErrOperationTimeout: "Request timed out, please try again",
	context.Canceled:              "Operation cancelled",
	context.DeadlineExceeded:      "Request timed out, please try again",
	ErrServiceUnavailable:         "Service unavailable. Please check your connection and try again",
}

// FormatError converts an error into a user-friendly message.
// It checks the error chain for known error types using errors.Is().
// In verbose mode, the full error chain is included.
func FormatError(err error, verbose bool) string {
	if err == nil {
		return ""
	}

	// Check for known error types using errors.Is()
	// This works even if the error has been wrapped with fmt.Errorf("...: %w", err)
	for targetErr, message := range errorMessages {
		if errors.Is(err, targetErr) {
			if verbose {
				return fmt.Sprintf("%s\nDetails: %v", message, err)
			}
			return message
		}
	}

	// Check for network errors
	if isNetworkError(err) {
		message := "Connection error, please check your internet connection"
		if verbose {
			return fmt.Sprintf("%s\nDetails: %v", message, err)
		}
		return message
	}

	// Use the error message directly if it's not a known type
	message := err.Error()
	if verbose {
		return fmt.Sprintf("%s\nDetails: %v", message, err)
	}
	return message
}

// isNetworkError checks if an error is network-related using the net.Error interface.
func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}
	return false
}

// WrapAuthError wraps an error with authentication context if it's an auth-related error.
// This provides consistent error messages across all services.
// Returns an error that includes both a human-readable message and the predefined error type
// for programmatic checking with errors.Is().
func WrapAuthError(operation string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, portalsdk.ErrUnauthorized) {
		return fmt.Errorf("%s failed - authentication expired or invalid. Run 'pinner auth login' to re-authenticate: %w", operation, ErrNotAuthenticated)
	}

	return fmt.Errorf("%s failed: %w", operation, err)
}

// WrapFileError wraps file-related errors with context about what operation failed.
func WrapFileError(operation, path string, err error) error {
	if err == nil {
		return nil
	}

	// Check for specific file system errors and wrap with predefined errors
	if os.IsNotExist(err) {
		return fmt.Errorf("%s failed for '%s': %w", operation, path, ErrFileNotFound)
	}
	if os.IsPermission(err) {
		return fmt.Errorf("%s failed for '%s': %w", operation, path, ErrPermissionDenied)
	}

	return fmt.Errorf("%s failed for '%s': %w", operation, path, err)
}
