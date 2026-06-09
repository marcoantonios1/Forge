package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/patch"
)

var ErrConfirmCancelled = errors.New("confirm: cancelled by user")

// AutoConfirmer always approves. Used for autonomous execution_policy or --yes flag.
type AutoConfirmer struct{}

func (AutoConfirmer) Confirm(_ *patch.PatchSet) (bool, error) { return true, nil }

// NullConfirmer always declines. Used in tests and dry-run mode.
type NullConfirmer struct{}

func (NullConfirmer) Confirm(_ *patch.PatchSet) (bool, error) { return false, nil }

// SafeConfirmer is the interactive confirmer for safe and supervised modes.
type SafeConfirmer struct {
	in        io.Reader
	out       io.Writer
	colour    bool
	yesToAll  bool
	emitter   events.Emitter
	sessionID string
}

func NewSafeConfirmer(
	in io.Reader,
	out io.Writer,
	colour bool,
	emitter events.Emitter,
	sessionID string,
) *SafeConfirmer {
	return &SafeConfirmer{
		in:        in,
		out:       out,
		colour:    colour,
		emitter:   emitter,
		sessionID: sessionID,
	}
}

func (c *SafeConfirmer) Confirm(ps *patch.PatchSet) (bool, error) {
	// TODO: per-tool-category permission grants (--allowed-tools flag) would hook in here.
	// TODO: auto-decline timeout after N seconds of no input would hook in here.

	if c.yesToAll {
		c.emitDecision(ps, "approved-all")
		return true, nil
	}

	fmt.Fprintln(c.out, RenderPreview(ps, c.colour))

	reader := bufio.NewReader(c.in)

	for {
		fmt.Fprint(c.out, "Apply this patch? [y]es / [n]o / [a]ll session  ")

		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, ErrConfirmCancelled
			}
			return false, err
		}

		input := strings.ToLower(strings.TrimSpace(line))

		// Raw Ctrl+C byte.
		if input == "\x03" {
			return false, ErrConfirmCancelled
		}

		switch input {
		case "y", "yes":
			c.emitDecision(ps, "approved")
			return true, nil
		case "n", "no", "":
			c.emitDecision(ps, "declined")
			return false, nil
		case "a", "all":
			c.yesToAll = true
			c.emitDecision(ps, "approved-all")
			return true, nil
		default:
			fmt.Fprintln(c.out, "Please enter y, n, or a.")
		}
	}
}

func (c *SafeConfirmer) emitDecision(ps *patch.PatchSet, decision string) {
	paths := make([]string, len(ps.Patches))
	for i, p := range ps.Patches {
		paths[i] = p.Path
	}
	c.emitter.Emit(events.Event{
		Type:      events.EventConfirmDecision,
		Timestamp: time.Now(),
		SessionID: c.sessionID,
		Payload: map[string]any{
			"session_id": c.sessionID,
			"decision":   decision,
			"files":      paths,
			"task_id":    ps.TaskID,
		},
	})
}
