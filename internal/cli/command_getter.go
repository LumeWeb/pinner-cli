package cli

import (
	"time"

	"github.com/urfave/cli/v3"
)

type flagGetter interface {
	String(name string) string
	Bool(name string) bool
}

type flagGetterWithInt interface {
	flagGetter
	Int(name string) int
}

type flagGetterWithIsSet interface {
	flagGetterWithInt
	IsSet(name string) bool
}

type flagGetterWithUint interface {
	flagGetterWithInt
	Uint(name string) uint
}

type flagGetterWithDuration interface {
	flagGetterWithUint
	Duration(name string) time.Duration
}

// commandGetter is the broadest interface satisfied by cliCommandWrapper.
// It encompasses all flag, arg, and CID access methods used across handlers.
type commandGetter interface {
	flagGetterWithIsSet
	argsGetter
	cidGetter
	Uint(name string) uint
	Duration(name string) time.Duration
}

type argsGetter interface {
	Args() cli.Args
}

type cidGetter interface {
	GetCID() string
}

type argsFlagGetter interface {
	argsGetter
	flagGetterWithInt
}

type cidFlagGetter interface {
	cidGetter
	flagGetterWithInt
}

// argsFlagGetterWithBool combines argsGetter and flagGetter for commands
// that need both positional args and Bool flag access (e.g., setConfig).
type argsFlagGetterWithBool interface {
	argsGetter
	flagGetter
}

// dnsCommandGetter combines all interfaces needed by DNS handlers.
type dnsCommandGetter interface {
	flagGetterWithIsSet
	argsGetter
	Uint(name string) uint
}

// benchCommandGetter combines all interfaces needed by bench handlers.
type benchCommandGetter interface {
	flagGetterWithDuration
	argsGetter
}

// websitesCommandGetter combines all interfaces needed by websites handlers.
type websitesCommandGetter interface {
	flagGetterWithIsSet
	argsGetter
}
