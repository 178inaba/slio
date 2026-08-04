package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/spf13/cobra"
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
	in := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	if _, err := fmt.Fprint(out, "Paste your Slack user OAuth token (xoxp-...): "); err != nil {
		return err
	}
	token, err := readLine(in)
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
		ok, err := confirm(out, in, fmt.Sprintf(
			"Profile %q is already registered for %s. Overwrite the stored token? [y/N]: ",
			existingName, result.Host))
		if err != nil {
			return err
		}
		if !ok {
			if _, err := fmt.Fprintln(out, "Aborted."); err != nil {
				return err
			}
			return nil
		}
		name = existingName
	} else {
		proposed := proposedProfileName(result.Host)
		if _, err := fmt.Fprintf(out, "Detected workspace %s. Register as profile %q? "+
			"Press Enter to accept, or type a different name: ", result.Host, proposed); err != nil {
			return err
		}
		typed, err := readLine(in)
		if err != nil {
			return fmt.Errorf("read profile name: %w", err)
		}
		name = proposed
		if typed != "" {
			name = typed
		}

		if other, ok := file.Profiles[name]; ok && other.Host != result.Host {
			ok, err := confirm(out, in, fmt.Sprintf(
				"Profile %q is already registered for a different workspace (%s). Overwrite it? [y/N]: ",
				name, other.Host))
			if err != nil {
				return err
			}
			if !ok {
				if _, err := fmt.Fprintln(out, "Aborted."); err != nil {
					return err
				}
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

	if _, err := fmt.Fprintf(out, "Registered profile %q for %s.\n", name, result.Host); err != nil {
		return err
	}
	if setDefault {
		if _, err := fmt.Fprintln(out, "Set as the default profile."); err != nil {
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

func confirm(out io.Writer, in *bufio.Reader, prompt string) (bool, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return false, err
	}
	line, err := readLine(in)
	if err != nil {
		return false, err
	}
	line = strings.ToLower(line)
	return line == "y" || line == "yes", nil
}
