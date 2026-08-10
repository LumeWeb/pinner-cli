package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetDefaultTimeout(t *testing.T) {
	c := &Config{}
	assert.Equal(t, time.Duration(DefaultTimeoutSeconds)*time.Second, c.GetDefaultTimeout())

	c = &Config{DefaultTimeout: 45 * time.Second}
	assert.Equal(t, 45*time.Second, c.GetDefaultTimeout())
}

func TestGetUploadTimeout(t *testing.T) {
	c := &Config{}
	assert.Equal(t, time.Duration(DefaultUploadTimeoutSeconds)*time.Second, c.GetUploadTimeout())

	c = &Config{UploadTimeout: 10 * time.Minute}
	assert.Equal(t, 10*time.Minute, c.GetUploadTimeout())
}

func TestGetSyncTimeout(t *testing.T) {
	c := &Config{}
	assert.Equal(t, time.Duration(DefaultSyncTimeoutSeconds)*time.Second, c.GetSyncTimeout())

	c = &Config{SyncTimeout: 2 * time.Minute}
	assert.Equal(t, 2*time.Minute, c.GetSyncTimeout())
}
