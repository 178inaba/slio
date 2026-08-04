package cmd

import (
	"bufio"
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

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Register a user token interactively",
	Args:  cobra.NoArgs,
	RunE:  runAuthLogin,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
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

	ctx, cancel := commandContext()
	defer cancel()
	result, err := slackClientFactory(token).AuthTest(ctx)
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

// promptToken reads the token without echoing it. A non-nil stdinFile is
// guaranteed to be a terminal by the guard in runAuthLogin; the test seam
// falls back to a plain line read.
func promptToken(out io.Writer, in *bufio.Reader, stdinFile *os.File) (string, error) {
	if _, err := fmt.Fprint(out, "Paste your Slack user OAuth token (xoxp-...): "); err != nil {
		return "", err
	}
	if stdinFile == nil {
		return readLine(in)
	}

	token, err := term.ReadPassword(int(stdinFile.Fd()))
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
