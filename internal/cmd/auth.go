package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// defaultSlackClientFactory builds a production Slack client. Tests
// override slackClientFactory to point at an httptest server instead.
func defaultSlackClientFactory(token string) *slackclient.Client {
	return slackclient.New(token)
}

var slackClientFactory = defaultSlackClientFactory

func newAuthCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(newAuthLoginCmd(g))

	return cmd
}

func newAuthLoginCmd(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Register a user token interactively",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthLogin(cmd, g)
		},
	}
}

func runAuthLogin(cmd *cobra.Command, g *globalFlags) error {
	// Prompts and status messages go to stderr so stdout stays reserved for
	// machine-readable output. auth login has none, so it writes no stdout.
	errOut := cmd.ErrOrStderr()

	// promptCtx is the signal context: a prompt has to abort on Ctrl-C, but
	// must not inherit --timeout, which is a deadline for the requests
	// rather than for how long the user may take to type. Keeping the two
	// under distinct names matters here — the commandContext call below is
	// in this same block, so a `ctx` declared now would be reassigned by it
	// rather than shadowed, and every later prompt would silently pick up
	// the deadline.
	promptCtx := cmd.Context()

	// Masking the token needs a real terminal. Non-TTY callers (agents, CI)
	// are pointed at the env var instead; a non-*os.File reader is the test
	// seam and reads answers line by line.
	stdin := cmd.InOrStdin()
	stdinFile, isTTY := terminalFile(stdin)
	if stdinFile != nil && !isTTY {
		return errors.New("auth login is interactive and needs a terminal; set SLIO_TOKEN instead")
	}

	// Safe to wrap even on the masked path: ReadPassword reads the file
	// descriptor directly, and this reader consumes nothing until the later
	// prompts, so the two never disagree about buffered bytes.
	in := bufio.NewReader(stdin)

	token, err := promptToken(promptCtx, errOut, in, stdinFile)
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	if !strings.HasPrefix(token, "xoxp-") {
		return fmt.Errorf("token must start with %q; bot tokens (%q) are not supported "+
			"because search.messages requires a user token", "xoxp-", "xoxb-")
	}

	reqCtx, cancel := commandContext(cmd, g.timeout)
	defer cancel()
	result, err := slackClientFactory(token).AuthTest(reqCtx)
	if err != nil {
		return fmt.Errorf("verify token: %w", err)
	}

	file, err := config.Load()
	if err != nil {
		return err
	}

	existingName := ""
	for name, p := range file.Profiles {
		if p.Host == result.Host {
			existingName = name
			break
		}
	}

	var name string
	if existingName != "" {
		aborted, err := confirmOrAbort(promptCtx, errOut, in, fmt.Sprintf(
			"Profile %q is already registered for %s. Overwrite the stored token? [y/N]: ",
			existingName, result.Host))
		if err != nil {
			return err
		}
		if aborted {
			return nil
		}
		name = existingName
	} else {
		proposed := proposedProfileName(result.Host)
		typed, err := promptLine(promptCtx, errOut, in, fmt.Sprintf(
			"Detected workspace %s. Register as profile %q? "+
				"Press Enter to accept, or type a different name: ", result.Host, proposed))
		if err != nil {
			return fmt.Errorf("read profile name: %w", err)
		}
		name = proposed
		if typed != "" {
			name = typed
		}

		if other, ok := file.Profiles[name]; ok && other.Host != result.Host {
			aborted, err := confirmOrAbort(promptCtx, errOut, in, fmt.Sprintf(
				"Profile %q is already registered for a different workspace (%s). Overwrite it? [y/N]: ",
				name, other.Host))
			if err != nil {
				return err
			}
			if aborted {
				return nil
			}
		}
	}

	file.Profiles[name] = config.Profile{Token: token, Host: result.Host, TeamID: result.TeamID}
	setDefault := file.DefaultProfile == ""
	if setDefault {
		file.DefaultProfile = name
	}
	// A signal can still land between the last prompt and the write. The
	// prompts aborting is the primary mechanism; this closes what is left
	// of the window.
	if err := promptCtx.Err(); err != nil {
		return err
	}
	if err := file.Save(); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(errOut, "Registered profile %q for %s.\n", name, result.Host); err != nil {
		return err
	}
	if setDefault {
		if _, err := fmt.Fprintln(errOut, "Set as the default profile."); err != nil {
			return err
		}
	}
	return nil
}

// proposedProfileName derives a profile name suggestion from a workspace
// host, e.g. "myws.slack.com" -> "myws".
func proposedProfileName(host string) string {
	name, _, _ := strings.Cut(host, ".")
	return name
}

// errPromptCancelled reports a prompt that ended because the command's
// context was cancelled. That context is the signal one, so Execute already
// prefixes "interrupted: " and exits 1; the wording avoids repeating the
// word rather than saying it twice.
var errPromptCancelled = errors.New("cancelled at the prompt")

// readCancellable runs a blocking read and gives up on it as soon as ctx is
// done, so a signal ends the command instead of waiting for the keystroke
// the user is no longer going to type.
//
// The read runs in a goroutine because one that has already blocked cannot
// be unblocked without closing the descriptor. On the cancel branch that
// goroutine stays parked in the read for the rest of the process's life,
// which is short by then: the channel is buffered so its send does not
// block once nobody is receiving, and no later prompt can race it because
// every caller returns as soon as this one is cancelled.
func readCancellable[T any](ctx context.Context, read func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}

	done := make(chan result, 1)
	go func() {
		value, err := read()
		done <- result{value: value, err: err}
	}()

	select {
	case <-ctx.Done():
		var zero T
		return zero, errPromptCancelled
	case res := <-done:
		return res.value, res.err
	}
}

// readLine reads one trimmed line. A cancelled read also ends the prompt
// line: nothing echoes the interrupt itself, so without this the error
// Execute prints would continue the prompt rather than start its own line.
func readLine(ctx context.Context, out io.Writer, r *bufio.Reader) (string, error) {
	line, err := readCancellable(ctx, func() (string, error) {
		return r.ReadString('\n')
	})
	if errors.Is(err, errPromptCancelled) {
		if _, err := fmt.Fprintln(out); err != nil {
			return "", err
		}
		return "", errPromptCancelled
	}
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptLine writes the prompt to out and reads one trimmed line.
func promptLine(ctx context.Context, out io.Writer, in *bufio.Reader, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	return readLine(ctx, out, in)
}

// promptToken reads the token without echoing it. A non-nil stdinFile is
// guaranteed to be a terminal by the guard in runAuthLogin; the test seam
// falls back to a plain line read.
func promptToken(ctx context.Context, out io.Writer, in *bufio.Reader, stdinFile *os.File) (string, error) {
	if _, err := fmt.Fprint(out, "Paste your Slack user OAuth token (xoxp-...): "); err != nil {
		return "", err
	}
	if stdinFile == nil {
		return readLine(ctx, out, in)
	}

	// ReadPassword clears ECHO and restores the terminal from its own
	// deferred call, which only runs once its read returns — never, on the
	// cancelled path below. Capturing the state here is what lets this
	// function put echo back instead of leaving the user's shell silent.
	fd := int(stdinFile.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}

	token, err := readCancellable(ctx, func() ([]byte, error) {
		return term.ReadPassword(fd)
	})
	if err != nil {
		if errors.Is(err, errPromptCancelled) {
			if err := term.Restore(fd, state); err != nil {
				return "", err
			}
			// Ctrl-C is not echoed with ECHO cleared, so end the prompt
			// line here as readLine does on the same path.
			if _, err := fmt.Fprintln(out); err != nil {
				return "", err
			}
			return "", errPromptCancelled
		}
		return "", err
	}
	// ReadPassword leaves the newline the user typed unechoed, so emit one to
	// keep the next message off the prompt line.
	if _, err := fmt.Fprintln(out); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(token)), nil
}

func confirm(ctx context.Context, out io.Writer, in *bufio.Reader, prompt string) (bool, error) {
	line, err := promptLine(ctx, out, in, prompt)
	if err != nil {
		return false, err
	}
	line = strings.ToLower(line)
	return line == "y" || line == "yes", nil
}

// confirmOrAbort wraps confirm with the "declined -> print Aborted. and
// report aborted=true" flow shared by both overwrite-confirmation prompts
// in runAuthLogin.
func confirmOrAbort(ctx context.Context, out io.Writer, in *bufio.Reader, prompt string) (aborted bool, err error) {
	ok, err := confirm(ctx, out, in, prompt)
	if err != nil {
		return false, err
	}
	if ok {
		return false, nil
	}
	if _, err := fmt.Fprintln(out, "Aborted."); err != nil {
		return false, err
	}
	return true, nil
}
