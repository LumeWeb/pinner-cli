package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ggwhite/go-masker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOutputFormatter(t *testing.T) {
	t.Run("returns humanFormatter when json is false", func(t *testing.T) {
		formatter := NewOutputFormatter(false, false, false, false)
		assert.IsType(t, &humanFormatter{}, formatter)
	})

	t.Run("returns jsonFormatter when json is true", func(t *testing.T) {
		formatter := NewOutputFormatter(true, false, false, false)
		assert.IsType(t, &jsonFormatter{}, formatter)
	})
}

func TestHumanFormatterPrint(t *testing.T) {
	t.Run("prints message when not quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Print("test message")

		assert.Equal(t, "test message\n", buf.String())
	})

	t.Run("suppresses output when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		formatter.Print("test message")

		assert.Empty(t, buf.String())
	})
}

func TestHumanFormatterPrintf(t *testing.T) {
	t.Run("prints formatted message when not quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Printf("hello %s", "world")

		assert.Equal(t, "hello world", buf.String())
	})

	t.Run("suppresses output when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		formatter.Printf("hello %s", "world")

		assert.Empty(t, buf.String())
	})
}

func TestHumanFormatterPrintfln(t *testing.T) {
	t.Run("prints formatted message with newline when not quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Printfln("hello %s", "world")

		assert.Equal(t, "hello world\n", buf.String())
	})

	t.Run("suppresses output when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		formatter.Printfln("hello %s", "world")

		assert.Empty(t, buf.String())
	})
}

func TestHumanFormatterPrintJSON(t *testing.T) {
	t.Run("prints JSON data", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		data := map[string]string{"key": "value"}
		err := formatter.PrintJSON(data)

		require.NoError(t, err)

		var result map[string]string
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "value", result["key"])
	})
}

func TestJSONFormatterPrint(t *testing.T) {
	t.Run("prints message as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Print("test message")

		var result map[string]string
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "test message", result["message"])
	})

	t.Run("suppresses output when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		formatter.Print("test message")

		assert.Empty(t, buf.String())
	})
}

func TestJSONFormatterPrintf(t *testing.T) {
	t.Run("prints formatted message as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Printf("hello %s", "world")

		var result map[string]string
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "hello world", result["message"])
	})
}

func TestJSONFormatterPrintfln(t *testing.T) {
	t.Run("prints formatted message as JSON (same as Printf)", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		formatter.Printfln("hello %s", "world")

		var result map[string]string
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "hello world", result["message"])
	})

	t.Run("suppresses output when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		formatter.Printfln("hello %s", "world")

		assert.Empty(t, buf.String())
	})
}

func TestJSONFormatterPrintJSON(t *testing.T) {
	t.Run("prints JSON data", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		data := map[string]string{"key": "value"}
		err := formatter.PrintJSON(data)

		require.NoError(t, err)

		var result map[string]string
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "value", result["key"])
	})
}

func TestPrintVerbose(t *testing.T) {
	t.Run("prints verbose message when verbose is true and not quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					verbose: true,
					quiet:   false,
					writer:  &buf,
				},
			},
		}

		formatter.PrintVerbose("verbose message")

		assert.Equal(t, "[verbose] verbose message\n", buf.String())
	})

	t.Run("suppresses verbose message when verbose is false", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					verbose: false,
					quiet:   false,
					writer:  &buf,
				},
			},
		}

		formatter.PrintVerbose("verbose message")

		assert.Empty(t, buf.String())
	})

	t.Run("suppresses verbose message when quiet is true", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					verbose: true,
					quiet:   true,
					writer:  &buf,
				},
			},
		}

		formatter.PrintVerbose("verbose message")

		assert.Empty(t, buf.String())
	})
}

func TestPrintVerbosef(t *testing.T) {
	t.Run("prints formatted verbose message when verbose is true", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					verbose: true,
					quiet:   false,
					writer:  &buf,
				},
			},
		}

		formatter.PrintVerbosef("hello %s", "world")

		assert.Equal(t, "[verbose] hello world\n", buf.String())
	})

	t.Run("suppresses formatted verbose message when verbose is false", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					verbose: false,
					quiet:   false,
					writer:  &buf,
				},
			},
		}

		formatter.PrintVerbosef("hello %s", "world")

		assert.Empty(t, buf.String())
	})
}

func TestHumanFormatterPrintTable(t *testing.T) {
	t.Run("prints table with headers and rows", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		headers := []string{"Name", "Age"}
		rows := [][]string{
			{"Alice", "30"},
			{"Bob", "25"},
		}

		formatter.PrintTable(headers, rows)
		result := buf.String()

		assert.Contains(t, result, "Name")
		assert.Contains(t, result, "Age")
		assert.Contains(t, result, "Alice")
		assert.Contains(t, result, "Bob")
		assert.Contains(t, result, "30")
		assert.Contains(t, result, "25")
	})

	t.Run("prints empty table", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		headers := []string{"Header"}
		rows := [][]string{}

		formatter.PrintTable(headers, rows)
		result := buf.String()

		assert.Contains(t, result, "Header")
	})

	t.Run("suppresses table when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		headers := []string{"Header"}
		rows := [][]string{{"Value"}}

		formatter.PrintTable(headers, rows)
		assert.Empty(t, buf.String())
	})
}

func TestHumanFormatterPrintList(t *testing.T) {
	t.Run("prints list with items", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		items := []string{"item1", "item2", "item3"}
		formatter.PrintList(items)
		result := buf.String()

		assert.Contains(t, result, "item1")
		assert.Contains(t, result, "item2")
		assert.Contains(t, result, "item3")
	})

	t.Run("prints empty list", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		items := []string{}
		formatter.PrintList(items)
		// Should not panic
	})

	t.Run("suppresses list when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &humanFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		items := []string{"item1", "item2"}
		formatter.PrintList(items)
		assert.Empty(t, buf.String())
	})
}

func TestJSONFormatterPrintTable(t *testing.T) {
	t.Run("prints table as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		headers := []string{"Name", "Age"}
		rows := [][]string{
			{"Alice", "30"},
			{"Bob", "25"},
		}

		formatter.PrintTable(headers, rows)

		type tableData struct {
			Type    string     `json:"type"`
			Headers []string   `json:"headers"`
			Rows    [][]string `json:"rows"`
		}
		var result tableData
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)

		assert.Equal(t, "table", result.Type)
		assert.Equal(t, headers, result.Headers)
		assert.Equal(t, rows, result.Rows)
	})

	t.Run("suppresses table when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		headers := []string{"Header"}
		rows := [][]string{{"Value"}}

		formatter.PrintTable(headers, rows)
		assert.Empty(t, buf.String())
	})
}

func TestJSONFormatterPrintList(t *testing.T) {
	t.Run("prints list as JSON", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  false,
					writer: &buf,
				},
			},
		}

		items := []string{"item1", "item2", "item3"}
		formatter.PrintList(items)

		type listData struct {
			Type  string   `json:"type"`
			Items []string `json:"items"`
		}
		var result listData
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)

		assert.Equal(t, "list", result.Type)
		assert.Equal(t, items, result.Items)
	})

	t.Run("suppresses list when quiet", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := &jsonFormatter{
			baseFormatter: baseFormatter{
				config: outputConfig{
					quiet:  true,
					writer: &buf,
				},
			},
		}

		items := []string{"item1", "item2"}
		formatter.PrintList(items)
		assert.Empty(t, buf.String())
	})
}

func TestNewOutputFormatterCombinations(t *testing.T) {
	testCases := []struct {
		name          string
		json          bool
		verbose       bool
		quiet         bool
		unmask        bool
		isHuman       bool
		shouldPrint   bool
		shouldVerbose bool
	}{
		{"default", false, false, false, false, true, true, false},
		{"json only", true, false, false, false, false, true, false},
		{"verbose only", false, true, false, false, true, true, true},
		{"quiet only", false, false, true, false, true, false, false},
		{"unmask only", false, false, false, true, true, true, false},
		{"json + verbose", true, true, false, false, false, true, false},
		{"json + quiet", true, false, true, false, false, false, false},
		{"verbose + quiet", false, true, true, false, true, false, false},
		{"all flags", true, true, true, true, false, false, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := NewOutputFormatter(tc.json, tc.verbose, tc.quiet, tc.unmask)

			// Check type via interface assertion
			if tc.isHuman {
				assert.IsType(t, &humanFormatter{}, output)
			} else {
				assert.IsType(t, &jsonFormatter{}, output)
			}

			// Check flags
			assert.Equal(t, tc.json, output.IsJSON())
			assert.Equal(t, tc.verbose, output.IsVerbose())
			assert.Equal(t, tc.quiet, output.IsQuiet())
			assert.Equal(t, tc.unmask, output.IsUnmask())

			// Check output behavior
			var buf bytes.Buffer
			output.SetWriter(&buf)

			output.Print("test message")
			output.PrintVerbose("verbose message")

			if tc.shouldPrint {
				assert.NotEmpty(t, buf.String(), "should print message")
			} else {
				assert.Empty(t, buf.String(), "should not print message")
			}

			if tc.shouldVerbose && !tc.quiet {
				assert.Contains(t, buf.String(), "verbose message", "should print verbose message")
			} else {
				assert.NotContains(t, buf.String(), "verbose message", "should not print verbose message")
			}
		})
	}
}

func TestOutputFormatterSetWriter(t *testing.T) {
	t.Run("set writer for human formatter", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)

		output.Print("test")
		assert.Equal(t, "test\n", buf.String())
	})

	t.Run("set writer for JSON formatter", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		output.Print("test")

		var result map[string]string
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "test", result["message"])
	})
}

func TestMaskToken(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"short token", "abc", "abc****"},
		{"8 chars", "12345678", "123456****"},
		{"9 chars", "123456789", "123456****"},
		{"normal token", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test", "eyJhbG****JIUzI1NiIsInR5cCI6IkpXVCJ9.test"},
		{"long token", strings.Repeat("a", 100), "aaaaaa****aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := masker.ID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMaskSensitive(t *testing.T) {
	humanFormatter := NewOutputFormatter(false, false, false, false).(*humanFormatter)
	jsonFormatter := NewOutputFormatter(true, false, false, false).(*jsonFormatter)

	testCases := []struct {
		name     string
		value    string
		key      string
		expected string
	}{
		{"empty value", "", "any_key", ""},
		{"token key", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "auth_token", "eyJhbG****JIUzI1NiIsInR5cCI6IkpXVCJ9"},
		{"auth key", "secret1234567890", "auth_key", "secret****567890"},
		{"password key", "mypassword", "password", "mypass****"},
		{"secret key", "mysecretvalue", "api_secret", "mysecr****lue"},
		{"key key", "my_api_key_value", "api_key", "my_api****_value"},
		{"email key", "test@example.com", "email", "tes****@example.com"},
		{"non-sensitive key", "regular_value", "name", "regular_value"},
		{"case insensitive token", "value123", "AUTHTOKEN", "value1****"},
		{"case insensitive password", "pass123", "UserPassword", "pass12****"},
	}

	for _, tc := range testCases {
		t.Run(tc.name+" human", func(t *testing.T) {
			result := humanFormatter.MaskSensitive(tc.value, tc.key)
			assert.Equal(t, tc.expected, result)
		})

		t.Run(tc.name+" json", func(t *testing.T) {
			result := jsonFormatter.MaskSensitive(tc.value, tc.key)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMaskSensitiveUnmaskMode(t *testing.T) {
	// Test that unmask mode disables masking
	humanFormatter := NewOutputFormatter(false, false, false, true).(*humanFormatter)
	jsonFormatter := NewOutputFormatter(true, false, false, true).(*jsonFormatter)

	testCases := []struct {
		name  string
		value string
		key   string
	}{
		{"token with unmask", "my_secret_token_12345", "auth_token"},
		{"password with unmask", "my_password", "password"},
		{"email with unmask", "test@example.com", "email"},
	}

	for _, tc := range testCases {
		t.Run(tc.name+" human", func(t *testing.T) {
			result := humanFormatter.MaskSensitive(tc.value, tc.key)
			// In unmask mode, should return original value
			assert.Equal(t, tc.value, result)
		})

		t.Run(tc.name+" json", func(t *testing.T) {
			result := jsonFormatter.MaskSensitive(tc.value, tc.key)
			// In unmask mode, should return original value
			assert.Equal(t, tc.value, result)
		})
	}
}

func TestAllTerminal(t *testing.T) {
	testCases := []struct {
		name     string
		rows     [][]string
		expected bool
	}{
		{
			name:     "all pinned",
			rows:     [][]string{{"cid1", "name1", "pinned", "created"}, {"cid2", "name2", "pinned", "created"}},
			expected: true,
		},
		{
			name:     "all failed",
			rows:     [][]string{{"cid1", "name1", "failed", "created"}, {"cid2", "name2", "failed", "created"}},
			expected: true,
		},
		{
			name:     "mix of pinned and failed",
			rows:     [][]string{{"cid1", "name1", "pinned", "created"}, {"cid2", "name2", "failed", "created"}},
			expected: true,
		},
		{
			name:     "contains queued",
			rows:     [][]string{{"cid1", "name1", "pinned", "created"}, {"cid2", "name2", "queued", "created"}},
			expected: false,
		},
		{
			name:     "contains pinning",
			rows:     [][]string{{"cid1", "name1", "pinned", "created"}, {"cid2", "name2", "pinning", "created"}},
			expected: false,
		},
		{
			name:     "all queued",
			rows:     [][]string{{"cid1", "name1", "queued", "created"}, {"cid2", "name2", "queued", "created"}},
			expected: false,
		},
		{
			name:     "empty rows",
			rows:     [][]string{},
			expected: true,
		},
		{
			name:     "row with less than 3 columns",
			rows:     [][]string{{"cid1", "name1"}, {"cid2", "name2", "pinned", "created"}},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := allTerminal(tc.rows)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestHumanFormatterWatch(t *testing.T) {
	t.Run("watches and updates data until terminal", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		callCount := 0
		fetcher := func(ctx context.Context) (any, error) {
			callCount++
			// First call returns queued (non-terminal)
			// Second call returns pinned (terminal)
			if callCount == 1 {
				return []string{"queued"}, nil
			}
			return []string{"pinned"}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			status := data.([]string)[0]
			return "", []string{"Status"}, [][]string{{status}}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		require.NoError(t, err)
		// Should have been called at least once (first tick)
		assert.GreaterOrEqual(t, callCount, 1)
	})

	t.Run("cancels on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel the context immediately
		cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return []string{"queued"}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{{"queued"}}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("handles empty results", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return []string{}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No items found")
	})

	t.Run("handles fetcher errors", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return nil, errors.New("fetch error")
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fetch error")
	})
}

func TestJSONFormatterWatch(t *testing.T) {
	t.Run("watches and updates data as JSON", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		callCount := 0
		fetcher := func(ctx context.Context) (any, error) {
			callCount++
			// First call returns queued (non-terminal)
			// Second call returns pinned (terminal)
			if callCount == 1 {
				return []string{"queued"}, nil
			}
			return []string{"pinned"}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			status := data.([]string)[0]
			return "test title", []string{"Status"}, [][]string{{status}}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		require.NoError(t, err)
		// Should have been called at least once (first tick)
		assert.GreaterOrEqual(t, callCount, 1)

		// Verify JSON output - parse all JSON objects in the buffer
		decoder := json.NewDecoder(&buf)
		foundWatch := false
		for decoder.More() {
			var result map[string]any
			err := decoder.Decode(&result)
			require.NoError(t, err)
			if result["type"] == "watch" && result["title"] == "test title" {
				foundWatch = true
				break
			}
		}
		assert.True(t, foundWatch, "Expected to find watch JSON object with type='watch' and title='test title'")
	})

	t.Run("cancels on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel the context immediately
		cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return []string{"queued"}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{{"queued"}}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("handles empty results", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return []string{}, nil
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No items found")
	})

	t.Run("handles fetcher errors", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		fetcher := func(ctx context.Context) (any, error) {
			return nil, errors.New("fetch error")
		}

		formatter := func(data any) (string, []string, [][]string) {
			return "", []string{"Status"}, [][]string{}
		}

		var buf bytes.Buffer
		output := NewOutputFormatter(true, false, false, false)
		output.SetWriter(&buf)

		err := output.Watch(ctx, fetcher, formatter)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fetch error")
	})
}
