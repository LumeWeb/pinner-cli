package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
	"go.lumeweb.com/pinner-cli/pkg/config"
	configmocks "go.lumeweb.com/pinner-cli/pkg/config/mocks"
)

// mockArgs implements cli.Args interface for testing
type mockArgs struct {
	args []string
}

func (m *mockArgs) First() string {
	if len(m.args) > 0 {
		return m.args[0]
	}
	return ""
}

func (m *mockArgs) Get(n int) string {
	if n >= 0 && n < len(m.args) {
		return m.args[n]
	}
	return ""
}

func (m *mockArgs) Len() int {
	return len(m.args)
}

func (m *mockArgs) Present() bool {
	return len(m.args) > 0
}

func (m *mockArgs) Slice() []string {
	return m.args
}

func (m *mockArgs) Tail() []string {
	if len(m.args) > 1 {
		return m.args[1:]
	}
	return []string{}
}

// newMockCommand creates a new mockCommand ready for builder method chaining.
func newMockCommand() *mockCommand {
	return &mockCommand{}
}

// mockCommand is a map-backed test double for commandGetter interfaces.
// It replaces hand-written mock command structs across test files.
type mockCommand struct {
	stringFields   map[string]string
	intFields      map[string]int
	int64Fields    map[string]int64
	uint64Fields   map[string]uint64
	uintFields     map[string]uint
	durationFields map[string]time.Duration
	floatFields    map[string]float64
	boolFields     map[string]bool
	stringSlices   map[string][]string
	isSetFields    map[string]bool
	args           cli.Args
	cid            string
}

func (m *mockCommand) withString(name, value string) *mockCommand {
	if m.stringFields == nil {
		m.stringFields = make(map[string]string)
	}
	m.stringFields[name] = value
	return m
}

func (m *mockCommand) withInt(name string, value int) *mockCommand {
	if m.intFields == nil {
		m.intFields = make(map[string]int)
	}
	m.intFields[name] = value
	return m
}

func (m *mockCommand) withInt64(name string, value int64) *mockCommand {
	if m.int64Fields == nil {
		m.int64Fields = make(map[string]int64)
	}
	m.int64Fields[name] = value
	return m
}

func (m *mockCommand) withUint64(name string, value uint64) *mockCommand {
	if m.uint64Fields == nil {
		m.uint64Fields = make(map[string]uint64)
	}
	m.uint64Fields[name] = value
	return m
}

func (m *mockCommand) withUint(name string, value uint) *mockCommand {
	if m.uintFields == nil {
		m.uintFields = make(map[string]uint)
	}
	m.uintFields[name] = value
	return m
}

func (m *mockCommand) withDuration(name string, value time.Duration) *mockCommand {
	if m.durationFields == nil {
		m.durationFields = make(map[string]time.Duration)
	}
	m.durationFields[name] = value
	return m
}

func (m *mockCommand) withFloat(name string, value float64) *mockCommand {
	if m.floatFields == nil {
		m.floatFields = make(map[string]float64)
	}
	m.floatFields[name] = value
	return m
}

func (m *mockCommand) withBool(name string, value bool) *mockCommand {
	if m.boolFields == nil {
		m.boolFields = make(map[string]bool)
	}
	m.boolFields[name] = value
	return m
}

func (m *mockCommand) withIsSet(name string, value bool) *mockCommand {
	if m.isSetFields == nil {
		m.isSetFields = make(map[string]bool)
	}
	m.isSetFields[name] = value
	return m
}

func (m *mockCommand) withStringSlice(name string, value []string) *mockCommand {
	if m.stringSlices == nil {
		m.stringSlices = make(map[string][]string)
	}
	m.stringSlices[name] = value
	return m
}

func (m *mockCommand) withArgs(args ...string) *mockCommand {
	m.args = &mockArgs{args: args}
	return m
}

func (m *mockCommand) withCID(cid string) *mockCommand {
	m.cid = cid
	return m
}

func (m *mockCommand) String(name string) string {
	if m.stringFields != nil {
		if v, ok := m.stringFields[name]; ok {
			return v
		}
	}
	return ""
}

func (m *mockCommand) Int(name string) int {
	if m.intFields != nil {
		if v, ok := m.intFields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Int64(name string) int64 {
	if m.int64Fields != nil {
		if v, ok := m.int64Fields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Uint64(name string) uint64 {
	if m.uint64Fields != nil {
		if v, ok := m.uint64Fields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Uint(name string) uint {
	if m.uintFields != nil {
		if v, ok := m.uintFields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Duration(name string) time.Duration {
	if m.durationFields != nil {
		if v, ok := m.durationFields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Float(name string) float64 {
	if m.floatFields != nil {
		if v, ok := m.floatFields[name]; ok {
			return v
		}
	}
	return 0
}

func (m *mockCommand) Bool(name string) bool {
	if m.boolFields != nil {
		if v, ok := m.boolFields[name]; ok {
			return v
		}
	}
	return false
}

func (m *mockCommand) IsSet(name string) bool {
	if m.isSetFields != nil {
		if v, ok := m.isSetFields[name]; ok {
			return v
		}
	}
	return false
}

func (m *mockCommand) StringSlice(name string) []string {
	if m.stringSlices != nil {
		if v, ok := m.stringSlices[name]; ok {
			return v
		}
	}
	return nil
}

func (m *mockCommand) Args() cli.Args {
	if m.args != nil {
		return m.args
	}
	return &mockArgs{}
}

func (m *mockCommand) GetCID() string {
	return m.cid
}

// Compile-time interface satisfaction checks
var (
	_ flagGetter             = (*mockCommand)(nil)
	_ flagGetterWithInt      = (*mockCommand)(nil)
	_ flagGetterWithIsSet    = (*mockCommand)(nil)
	_ flagGetterWithUint     = (*mockCommand)(nil)
	_ flagGetterWithDuration = (*mockCommand)(nil)
	_ commandGetter          = (*mockCommand)(nil)
	_ argsGetter             = (*mockCommand)(nil)
	_ cidGetter              = (*mockCommand)(nil)
	_ argsFlagGetter         = (*mockCommand)(nil)
	_ cidFlagGetter          = (*mockCommand)(nil)
	_ dnsCommandGetter       = (*mockCommand)(nil)
	_ benchCommandGetter     = (*mockCommand)(nil)
	_ websitesCommandGetter  = (*mockCommand)(nil)
)

// newTestOutput creates a human-readable Output for testing.
func newTestOutput() Output {
	return NewOutputFormatter(false, false, false, false)
}

func newTestConfigMgr(t *testing.T) *configmocks.MockManager {
	m := configmocks.NewMockManager(t)
	m.EXPECT().Config().Return(&config.Config{
		BaseEndpoint: "pinner.xyz",
		Secure:       true,
		AuthToken:    "test-token",
	}).Maybe()
	return m
}

func getFlagNames(cmd *cli.Command) []string {
	names := make([]string, len(cmd.Flags))
	for i, f := range cmd.Flags {
		names[i] = f.Names()[0]
	}
	return names
}

func getSubcommandNames(cmd *cli.Command) []string {
	names := make([]string, len(cmd.Commands))
	for i, c := range cmd.Commands {
		names[i] = c.Name
	}
	return names
}

// Compile-time interface satisfaction checks for mockCommand.
var _ flagGetter = (*mockCommand)(nil)
var _ flagGetterWithInt = (*mockCommand)(nil)
var _ flagGetterWithIsSet = (*mockCommand)(nil)
var _ flagGetterWithUint = (*mockCommand)(nil)
var _ flagGetterWithDuration = (*mockCommand)(nil)
var _ commandGetter = (*mockCommand)(nil)
var _ argsGetter = (*mockCommand)(nil)
var _ cidGetter = (*mockCommand)(nil)
var _ argsFlagGetter = (*mockCommand)(nil)
var _ cidFlagGetter = (*mockCommand)(nil)
var _ dnsCommandGetter = (*mockCommand)(nil)
var _ benchCommandGetter = (*mockCommand)(nil)
var _ websitesCommandGetter = (*mockCommand)(nil)

func failingConfigMgrFactory() ConfigManagerFactory {
	return func() (config.Manager, error) {
		return nil, errors.New("config error")
	}
}
