package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

	token, err := promptToken(errOut, in, stdinFile)
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
		aborted, err := confirmOrAbort(errOut, in, fmt.Sprintf(
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
		typed, err := promptLine(errOut, in, fmt.Sprintf(
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
			aborted, err := confirmOrAbort(errOut, in, fmt.Sprintf(
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

// readLine reads one trimmed line. An interrupt here needs no line-closing
// output: ECHO is on at a plain-text prompt, so the line discipline echoes
// ^C itself.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptLine writes the prompt to out and reads one trimmed line.
func promptLine(out io.Writer, in *bufio.Reader, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	return readLine(in)
}

// interruptSignals lists the signals the terminal guard catches. It is a
// function so tests can assert the registered set without a second copy of
// the list to keep in step.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// terminalGuard puts the terminal back and then re-raises, so an interrupt
// during the masked token read ends the process by the signal the user sent
// rather than through an error report.
//
// Nothing else in slio needs the signal caught: dying immediately is
// already the wanted behaviour everywhere else, and the modified terminal
// is the only thing that would outlive the process badly.
//
// reset, restore and reraise are fields rather than direct calls because
// none of them can run under test — restore needs a real terminal, and
// reraise ends the process. newTerminalGuard fills them with the real ones.
type terminalGuard struct {
	out     io.Writer
	fd      int
	state   *term.State
	reset   func(...os.Signal)
	restore func(int, *term.State) error
	reraise func(syscall.Signal) // does not return
}

func newTerminalGuard(out io.Writer, fd int, state *term.State) *terminalGuard {
	return &terminalGuard{
		out:     out,
		fd:      fd,
		state:   state,
		reset:   signal.Reset,
		restore: term.Restore,
		reraise: reraise,
	}
}

// arm starts catching the interrupt signals and returns the call that stops
// it again. The guard handles at most one signal: the process is meant to
// die of it, so there is no second one to serve.
//
// Disarming is best-effort against a signal that has already been taken,
// and losing that race is harmless — the user pressed Ctrl-C, and the
// process dying is the wanted outcome either way.
func (g *terminalGuard) arm() (disarm func()) {
	// Buffered, as signal.Notify requires: the runtime drops a signal
	// rather than blocking on the send.
	received := make(chan os.Signal, 1)
	signal.Notify(received, interruptSignals()...)

	disarmed := make(chan struct{})
	go func() {
		select {
		case <-disarmed:
		case sig := <-received:
			g.handle(sig)
		}
	}()

	return func() {
		signal.Stop(received)
		close(disarmed)
	}
}

// handle runs the clean-up the guard exists for. The order is load-bearing:
// resetting the handlers first is what lets a second signal end the process
// at once instead of queueing behind the restore, which the Command Line
// Interface Guidelines ask for. The cost is that a second signal landing in
// the microseconds before the restore leaves echo off; `stty sane` recovers
// it, and re-arming to close that window would reinstate the very problem
// the reset is here to avoid.
//
// Every signal interruptSignals returns is reset, not just the one that
// arrived: signal.Reset is variadic and resets only what it is given, so a
// SIGTERM following a SIGINT would otherwise land in a channel nobody reads.
func (g *terminalGuard) handle(sig os.Signal) {
	g.reset(interruptSignals()...)

	// Both errors are dropped rather than reported: an interrupted run
	// prints no failure report, and there is no branch left to take with
	// the process about to die of the signal.
	_ = g.restore(g.fd, g.state)
	// Nothing echoes the interrupt with ECHO cleared, so this newline is
	// what ends the prompt's line.
	_, _ = fmt.Fprintln(g.out)

	// A signal that is not a syscall.Signal carries no number to build a
	// status from. interruptSignals yields only ones that are, so this
	// cannot happen in practice.
	s, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	// The guard runs on its own goroutine rather than on the path that
	// returns from Execute, so this ends the process instead of a code.
	g.reraise(s)
}

// promptToken reads the token without echoing it. A non-nil stdinFile is
// guaranteed to be a terminal by the check in runAuthLogin; the test seam
// falls back to a plain line read.
func promptToken(out io.Writer, in *bufio.Reader, stdinFile *os.File) (string, error) {
	if _, err := fmt.Fprint(out, "Paste your Slack user OAuth token (xoxp-...): "); err != nil {
		return "", err
	}
	if stdinFile == nil {
		return readLine(in)
	}

	// ReadPassword clears ECHO and restores the terminal from its own
	// deferred call, which only runs once its read returns — never, when a
	// signal ends the process mid-read. Capturing the state here is what
	// lets the guard put echo back instead of leaving the user's shell
	// silent.
	fd := int(stdinFile.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}

	// term.GetState only reads, so arming after it still leaves the guarded
	// window a strict superset of the modified one: a signal arriving
	// before ReadPassword clears ECHO restores a state that was never
	// changed, which is a no-op. There is no window in which the terminal
	// is modified and unguarded.
	disarm := newTerminalGuard(out, fd, state).arm()
	token, err := term.ReadPassword(fd)
	disarm()
	if err != nil {
		return "", err
	}
	// ReadPassword leaves the newline the user typed unechoed, so emit one to
	// keep the next message off the prompt line.
	if _, err := fmt.Fprintln(out); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(token)), nil
}

func confirm(out io.Writer, in *bufio.Reader, prompt string) (bool, error) {
	line, err := promptLine(out, in, prompt)
	if err != nil {
		return false, err
	}
	line = strings.ToLower(line)
	return line == "y" || line == "yes", nil
}

// confirmOrAbort wraps confirm with the "declined -> print Aborted. and
// report aborted=true" flow shared by both overwrite-confirmation prompts
// in runAuthLogin.
func confirmOrAbort(out io.Writer, in *bufio.Reader, prompt string) (aborted bool, err error) {
	ok, err := confirm(out, in, prompt)
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
