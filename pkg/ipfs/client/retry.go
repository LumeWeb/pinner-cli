package ipfsclient

import (
	"context"
	"time"

	"github.com/avast/retry-go/v4"
)

// RetryOptions returns standard retry configuration for API calls.
// This is exported for use in CLI layer services that need retry logic.
func RetryOptions(ctx context.Context) []retry.Option {
	return []retry.Option{
		retry.Attempts(3),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.MaxJitter(5 * time.Second),
		retry.MaxDelay(30 * time.Second),
	}
}

// retryOptions is the internal version used within the ipfs/client package.
func retryOptions(ctx context.Context) []retry.Option {
	return RetryOptions(ctx)
}
