package gitremote

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage"
)

// supportedCaps are the capabilities the helper advertises (option / fetch /
// push). The helper serves only the dumb fetch/push protocol — it does not
// implement connect/import/export or wire-protocol v2.
var supportedCaps = []string{"option", "fetch", "push"}

// okOptions are transport options we accept and simply acknowledge; they only
// expose knobs the engine does not need to act on (verbosity, progress, depth
// stubs, force/cloning hints...). Everything else is reported unsupported, and
// object-format in particular is rejected so Git falls back to sha1 (go-git's
// hash width).
var okOptions = map[string]bool{
	"verbosity":          true,
	"progress":           true,
	"depth":              true,
	"deepen-since":       true,
	"deepen-not":         true,
	"deepen-relative":    true,
	"followtags":         true,
	"dry-run":            true,
	"servpath":           true,
	"check-connectivity": true,
	"force":              true,
	"cloning":            true,
	"update-shallow":     true,
	"pushcert":           true,
	"push-option":        true,
	"from-promisor":      true,
	"no-dependents":      true,
	"atomic":             true,
}

// Handle runs the remote-helper protocol over in/out against store, transferring
// objects through the local go-git object store (the repository Git pointed the
// helper at via GIT_DIR/cwd). Normal protocol output goes to out; diagnostics go
// to errw. It returns a non-nil error only for fatal conditions (a failed fetch,
// malformed input); push failures are reported per-ref on the protocol stream
// rather than as stream-wide errors, per gitremote-helpers(7).
func Handle(ctx context.Context, local storage.Storer, store Store, in io.Reader, out, errw io.Writer) error {
	if store == nil {
		return fmt.Errorf("gitremote: nil store")
	}
	h := &handler{local: local, store: store, out: out, errw: errw}
	if errw == nil {
		h.errw = io.Discard
	}

	reader := bufio.NewReader(in)
	for {
		line, err := readLine(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(line) == "" {
			// A blank line at the top level terminates the command stream.
			return nil
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "capabilities":
			h.writeCapabilities()
		case "list":
			forPush := len(fields) > 1 && fields[1] == "for-push"
			if err := h.handleList(ctx, forPush); err != nil {
				return err
			}
		case "option":
			name := ""
			value := ""
			if len(fields) > 1 {
				name = fields[1]
			}
			if len(fields) > 2 {
				value = fields[2]
			}
			h.handleOption(name, value)
		case "fetch":
			cmds := [][2]string{{fields[1], fields[2]}}
			// A fetch batch continues until a blank line.
			for {
				l2, err := readLine(reader)
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				if strings.TrimSpace(l2) == "" {
					break
				}
				f2 := strings.Fields(l2)
				if len(f2) < 3 || f2[0] != "fetch" {
					return fmt.Errorf("gitremote: malformed fetch command %q", l2)
				}
				cmds = append(cmds, [2]string{f2[1], f2[2]})
			}
			if err := h.handleFetch(ctx, cmds); err != nil {
				return err
			}
		case "push":
			// A push command is `push <refspec>` — one token like +a:b or :b.
			cmds := [][1]string{{fields[1]}}
			for {
				l2, err := readLine(reader)
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				if strings.TrimSpace(l2) == "" {
					break
				}
				f2 := strings.Fields(l2)
				if f2[0] != "push" {
					return fmt.Errorf("gitremote: malformed push command %q", l2)
				}
				cmds = append(cmds, [1]string{f2[1]})
			}
			if err := h.handlePush(ctx, cmds); err != nil {
				return err
			}
		default:
			return fmt.Errorf("gitremote: unknown command %q", fields[0])
		}
	}
}

// handler carries the protocol engine's per-invocation state.
type handler struct {
	local storage.Storer
	store Store
	out   io.Writer
	errw  io.Writer

	// refsForPush is the most recent `list for-push` result. It records the
	// remote's current refs so a push batch can compute the incremental pack
	// against the archived state.
	refsForPush []Ref
}

func (h *handler) writeCapabilities() {
	for _, c := range supportedCaps {
		fmt.Fprintf(h.out, "%s\n", c)
	}
	fmt.Fprintf(h.out, "\n")
}

func (h *handler) handleList(ctx context.Context, forPush bool) error {
	refs, err := h.store.List(ctx, forPush)
	if err != nil {
		return fmt.Errorf("gitremote: list: %w", err)
	}
	if forPush {
		h.refsForPush = refs
	}
	for _, r := range refs {
		val := r.Hash.String()
		if r.Hash == plumbing.ZeroHash {
			val = "?"
		}
		fmt.Fprintf(h.out, "%s %s\n", val, r.Name)
	}
	fmt.Fprintf(h.out, "\n")
	return nil
}

func (h *handler) handleOption(name, value string) {
	if name == "object-format" {
		// We only speak sha1; asking for object-format (sha256) is unsupported,
		// which makes Git fall back to sha1.
		fmt.Fprintf(h.out, "unsupported\n")
		return
	}
	if okOptions[name] {
		fmt.Fprintf(h.out, "ok\n")
		return
	}
	fmt.Fprintf(h.out, "unsupported\n")
}

func (h *handler) handleFetch(ctx context.Context, cmds [][2]string) error {
	for _, cmd := range cmds {
		sha1, name := cmd[0], cmd[1]
		if !hexHashRE.MatchString(sha1) {
			return fmt.Errorf("gitremote: fetch: invalid object %q", sha1)
		}
		rc, err := h.store.Download(ctx, name, sha1)
		if err != nil {
			return fmt.Errorf("gitremote: fetch %s: %w", name, err)
		}
		if err := IngestPack(h.local, rc); err != nil {
			if rc != nil {
				rc.Close()
			}
			return fmt.Errorf("gitremote: fetch %s: %w", name, err)
		}
		if rc != nil {
			rc.Close()
		}
	}
	// Single blank line marks the whole batch complete.
	fmt.Fprintf(h.out, "\n")
	return nil
}

func (h *handler) handlePush(ctx context.Context, cmds [][1]string) error {
	// Remote state before this batch (from `list for-push`), keyed by name.
	remote := map[string]plumbing.Hash{}
	for _, r := range h.refsForPush {
		if r.Hash != plumbing.ZeroHash {
			remote[r.Name] = r.Hash
		}
	}

	for _, cmd := range cmds {
		refspec := cmd[0]
		colon := strings.IndexByte(refspec, ':')
		if colon < 0 {
			fmt.Fprintf(h.out, "error %s missing destination\n", refspec)
			continue
		}
		src := strings.TrimPrefix(refspec[:colon], "+")
		dst := refspec[colon+1:]

		// Delete ref push: empty source.
		if src == "" {
			if err := h.store.Delete(ctx, dst); err != nil {
				fmt.Fprintf(h.out, "error %s %s\n", dst, quoteWhy(err))
			} else {
				fmt.Fprintf(h.out, "ok %s\n", dst)
			}
			continue
		}

		srcHash, err := resolveRevision(h.local, src)
		if err != nil {
			fmt.Fprintf(h.out, "error %s %s\n", dst, quoteWhy(err))
			continue
		}

		// Incremental pack: new objects minus what the remote already had.
		var haves []plumbing.Hash
		if old, ok := remote[dst]; ok {
			haves = append(haves, old)
		}
		pack, err := EncodeIncrementalPack(h.local, []plumbing.Hash{srcHash}, haves)
		if err != nil {
			fmt.Fprintf(h.out, "error %s %s\n", dst, quoteWhy(err))
			continue
		}
		if err := h.store.Upload(ctx, dst, srcHash.String(), bytes.NewReader(pack)); err != nil {
			fmt.Fprintf(h.out, "error %s %s\n", dst, quoteWhy(err))
			continue
		}
		fmt.Fprintf(h.out, "ok %s\n", dst)
	}
	// Blank line terminates the status report.
	fmt.Fprintf(h.out, "\n")
	return nil
}

// quoteWhy renders an error for the `error <dst> <why>` line, quoting it in C
// style when it contains a newline (per the protocol spec).
func quoteWhy(err error) string {
	msg := err.Error()
	if strings.ContainsAny(msg, "\n\r\"") {
		return strconvQuote(msg)
	}
	return msg
}

func strconvQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// readLine reads one line from r without the trailing newline; io.EOF is
// returned when the stream ends cleanly.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}
