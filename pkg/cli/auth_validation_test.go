package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateJWTFormat(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid JWT with 3 parts",
			token:   "header.payload.signature",
			wantErr: false,
		},
		{
			name:    "valid JWT with minimal parts",
			token:   "x.y.z",
			wantErr: false,
		},
		{
			name:    "two parts fails",
			token:   "header.payload",
			wantErr: true,
			errMsg:  "token must have 3 parts separated by dots",
		},
		{
			name:    "four parts fails",
			token:   "a.b.c.d",
			wantErr: true,
			errMsg:  "token must have 3 parts separated by dots",
		},
		{
			name:    "empty string fails",
			token:   "",
			wantErr: true,
			errMsg:  "token must have 3 parts separated by dots",
		},
		{
			name:    "empty first part fails",
			token:   ".payload.signature",
			wantErr: true,
			errMsg:  "token part 1 is empty",
		},
		{
			name:    "empty second part fails",
			token:   "header..signature",
			wantErr: true,
			errMsg:  "token part 2 is empty",
		},
		{
			name:    "empty third part fails",
			token:   "header.payload.",
			wantErr: true,
			errMsg:  "token part 3 is empty",
		},
		{
			name:    "single part fails",
			token:   "justastring",
			wantErr: true,
			errMsg:  "token must have 3 parts separated by dots",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJWTFormat(tt.token)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
