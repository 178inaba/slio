package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/slackclient"
)

func newAuthTestServer(t *testing.T, host, teamID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"ok":true,"url":"https://%s/","team_id":"%s"}`, host, teamID)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubSlackClientFactory points the factory at srv and reports whether it
// was called, so a test can assert that a run stopped before reaching the
// API.
func stubSlackClientFactory(t *testing.T, srv *httptest.Server) *atomic.Bool {
	t.Helper()
	return stubSlackClientFactoryAt(t, srv.URL+"/")
}

// noSlackClientFactory stubs the factory for runs that must not reach the
// API at all. It records the call instead of failing on the spot: the
// command runs in its own goroutine, where t.Fatal would kill the run and
// surface as runAuthLoginWithContext's timeout rather than as the real
// cause. The dead address means a call that should not have happened fails
// instead of panicking, leaving the returned flag to report it.
func noSlackClientFactory(t *testing.T) *atomic.Bool {
	t.Helper()
	return stubSlackClientFactoryAt(t, "http://127.0.0.1:0/")
}

func stubSlackClientFactoryAt(t *testing.T, apiURL string) *atomic.Bool {
	t.Helper()
	var called atomic.Bool
	orig := slackClientFactory
	slackClientFactory = func(token string) *slackclient.Client {
		called.Store(true)
		return slackclient.New(token, slackclient.WithAPIURL(apiURL))
	}
	t.Cleanup(func() { slackClientFactory = orig })
	return &called
}

// commandReturnTimeout bounds how long a test waits for `auth login` to
// return after an interrupt. An interrupted prompt is meant to end the
// command at once, so this only ever fires on a regression; it is generous
// so a loaded CI machine does not trip it.
const commandReturnTimeout = 10 * time.Second

// runAuthLoginWithContext drives `auth login` with the given context and
// stdin, and reports what Execute would: the stderr text, the exit code,
// and the described error. stdout is asserted empty for every caller: the
// command is interactive only, so nothing it prints belongs on the stream
// that carries machine-readable output. stdin is an io.Reader rather than a
// string because the non-TTY case needs a real *os.File.
//
// The command runs in a goroutine because an interrupted prompt is supposed
// to return on its own; called directly, a run that does not would hang the
// whole package until the test binary panics.
func runAuthLoginWithContext(t *testing.T, ctx context.Context, stdin io.Reader) (stderr string, exit int, err error) {
	t.Helper()
	root, out, errOut := newTestRoot(t)
	root.SetIn(stdin)
	root.SetArgs([]string{"auth", "login"})

	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	select {
	case err = <-done:
	case <-time.After(commandReturnTimeout):
		t.Fatal("the command did not return after the interrupt")
	}

	if out.Len() > 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	// Execute describes the error only when there is one; describing a nil
	// error would turn a successful run into a failure here.
	if err != nil {
		err = describeContextError(ctx, err, defaultTimeout)
	}
	return errOut.String(), exitCode(ctx, err), err
}

// runAuthLoginForTest drives `auth login` to completion with the given
// stdin and returns what it wrote to stderr.
func runAuthLoginForTest(t *testing.T, stdin io.Reader) (string, error) {
	t.Helper()
	stderr, _, err := runAuthLoginWithContext(t, context.Background(), stdin)
	return stderr, err
}

// scriptedStdin serves one answer per Read so a test can pick the prompt
// the user interrupts. interrupt fires as the read at interruptAt is
// served; when that read has no answer left — the user pressed Ctrl-C
// instead of typing — the reader then blocks the way a real terminal read
// does. Go registers its signal handlers with SA_RESTART, so a signal does
// not make a pending read return; returning io.EOF here would end the
// prompt on its own and the command would abort with or without the fix
// under test.
type scriptedStdin struct {
	answers     []string // each ends with "\n"
	interruptAt int
	interrupt   func()
	release     <-chan struct{} // closed by the test, releasing a parked read

	reads int
}

func (s *scriptedStdin) Read(p []byte) (int, error) {
	read := s.reads
	s.reads++
	if read == s.interruptAt {
		s.interrupt()
	}
	if read < len(s.answers) {
		return copy(p, s.answers[read]), nil
	}
	<-s.release
	return 0, io.EOF
}

func newScriptedStdin(t *testing.T, interrupt func(), interruptAt int, answers []string) *scriptedStdin {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return &scriptedStdin{
		answers:     answers,
		interruptAt: interruptAt,
		interrupt:   interrupt,
		release:     release,
	}
}

// newInterruptedStdin answers the prompts in order and then interrupts at
// the next one, standing in for a user who presses Ctrl-C instead of typing.
func newInterruptedStdin(t *testing.T, interrupt func(), answers ...string) *scriptedStdin {
	t.Helper()
	return newScriptedStdin(t, interrupt, len(answers), answers)
}

// newStdinInterruptedWithLastAnswer interrupts as the final answer is
// served, standing in for a signal that lands in the window between the
// last prompt and the write.
func newStdinInterruptedWithLastAnswer(t *testing.T, interrupt func(), answers ...string) *scriptedStdin {
	t.Helper()
	return newScriptedStdin(t, interrupt, len(answers)-1, answers)
}

// assertConfigUnchanged checks that the config file still holds what the
// test seeded, or was never created when nothing was seeded.
func assertConfigUnchanged(t *testing.T, seed *config.File) {
	t.Helper()
	path, err := config.Path()
	if err != nil {
		t.Fatalf("config.Path() error = %v", err)
	}

	if seed == nil {
		switch _, err := os.Stat(path); {
		case err == nil:
			f, _ := config.Load()
			t.Errorf("config file exists (profiles = %+v), want it never created", f.Profiles)
		case !os.IsNotExist(err):
			t.Fatalf("os.Stat(%s) error = %v", path, err)
		}
		return
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !reflect.DeepEqual(f, seed) {
		t.Errorf("config = %+v, want it unchanged at %+v", f, seed)
	}
}

func TestAuthLoginRegistersNewProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	stderr, err := runAuthLoginForTest(t, strings.NewReader("xoxp-abc\n\n"))
	if err != nil {
		t.Fatalf("runAuthLoginForTest() error = %v, stderr = %s", err, stderr)
	}

	for _, want := range []string{
		"Paste your Slack user OAuth token",
		"Register as profile",
		`Registered profile "myws"`,
		"Set as the default profile.",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if f.DefaultProfile != "myws" {
		t.Errorf("DefaultProfile = %q, want myws", f.DefaultProfile)
	}
	p, ok := f.Profiles["myws"]
	if !ok {
		t.Fatalf("profile %q not registered", "myws")
	}
	if p.Token != "xoxp-abc" || p.Host != "myws.slack.com" || p.TeamID != "T1" {
		t.Errorf("profile = %+v, want {xoxp-abc myws.slack.com T1}", p)
	}
}

func TestAuthLoginCustomProfileName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	if _, err := runAuthLoginForTest(t, strings.NewReader("xoxp-abc\ncustomname\n")); err != nil {
		t.Fatalf("runAuthLoginForTest() error = %v", err)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if _, ok := f.Profiles["customname"]; !ok {
		t.Fatalf("profile %q not registered; profiles = %+v", "customname", f.Profiles)
	}
}

func TestAuthLoginRejectsNonUserToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// No server stub: the prefix check has to short-circuit before any API
	// call, so the factory must stay untouched.
	called := noSlackClientFactory(t)

	_, err := runAuthLoginForTest(t, strings.NewReader("xoxb-bot-token\n"))
	if err == nil {
		t.Fatal("runAuthLoginForTest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error = %v, want mention of xoxp- prefix", err)
	}
	if called.Load() {
		t.Error("slackClientFactory was called for a rejected token")
	}

	if _, statErr := config.Load(); statErr == nil {
		f, _ := config.Load()
		if len(f.Profiles) != 0 {
			t.Errorf("profiles = %+v, want none saved", f.Profiles)
		}
	}
}

func TestAuthLoginRejectsNonTTYStdin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// A pipe is an *os.File that is not a terminal — what stdin looks like
	// under `echo ... | slio auth login`. The masked prompt cannot run there,
	// so the command must refuse before reading anything.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("closing the read end of the pipe: %v", err)
		}
	})
	if _, err := w.WriteString("xoxp-piped\n"); err != nil {
		t.Fatalf("writing the token to the pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the write end of the pipe: %v", err)
	}

	called := noSlackClientFactory(t)

	_, err = runAuthLoginForTest(t, r)
	if err == nil {
		t.Fatal("runAuthLoginForTest() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "SLIO_TOKEN") {
		t.Errorf("error = %v, want it to point at SLIO_TOKEN", err)
	}
	if called.Load() {
		t.Error("slackClientFactory was called without a terminal")
	}

	f, loadErr := config.Load()
	if loadErr != nil {
		t.Fatalf("config.Load() error = %v", loadErr)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("profiles = %+v, want none saved", f.Profiles)
	}
}

func TestAuthLoginAuthTestFailureDoesNotSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	_, err := runAuthLoginForTest(t, strings.NewReader("xoxp-bad\n"))
	if err == nil {
		t.Fatal("runAuthLoginForTest() error = nil, want error")
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("profiles = %+v, want none saved", f.Profiles)
	}
}

func TestAuthLoginReRegisterSameWorkspaceConfirmed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	existing := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-old", Host: "myws.slack.com", TeamID: "T1"},
		},
	}
	if err := existing.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	stderr, err := runAuthLoginForTest(t, strings.NewReader("xoxp-new\ny\n"))
	if err != nil {
		t.Fatalf("runAuthLoginForTest() error = %v, stderr = %s", err, stderr)
	}
	if !strings.Contains(stderr, "Overwrite the stored token?") {
		t.Errorf("stderr = %q, want the overwrite confirmation prompt", stderr)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	p := f.Profiles["myws"]
	if p.Token != "xoxp-new" {
		t.Errorf("Token = %q, want xoxp-new (overwritten)", p.Token)
	}
	if len(f.Profiles) != 1 {
		t.Errorf("Profiles = %+v, want exactly one entry (myws)", f.Profiles)
	}
}

func TestAuthLoginReRegisterSameWorkspaceDeclined(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	existing := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-old", Host: "myws.slack.com", TeamID: "T1"},
		},
	}
	if err := existing.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	stderr, err := runAuthLoginForTest(t, strings.NewReader("xoxp-new\nn\n"))
	if err != nil {
		t.Fatalf("runAuthLoginForTest() error = %v, stderr = %s", err, stderr)
	}
	if !strings.Contains(stderr, "Aborted") {
		t.Errorf("stderr = %q, want mention of Aborted", stderr)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if f.Profiles["myws"].Token != "xoxp-old" {
		t.Errorf("Token = %q, want xoxp-old (unchanged)", f.Profiles["myws"].Token)
	}
}

// assertInterrupted checks the outcome every interrupted run shares: the
// command fails, exits 1, and says it was interrupted without dragging in
// the token-format complaint that an empty prompt answer used to produce.
func assertInterrupted(t *testing.T, stderr string, exit int, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want an interruption; stderr = %s", stderr)
	}
	if exit != 1 {
		t.Errorf("exit code = %d, want 1", exit)
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("error = %v, want it to read as an interruption", err)
	}
	if strings.Contains(err.Error(), "must start with") {
		t.Errorf("error = %v, want no token-format complaint", err)
	}
}

// TestAuthLoginInterruptedAtPromptSavesNothing covers Ctrl-C at each prompt.
// The profile name case is the one that used to save the profile and exit 0:
// auth.test has already succeeded by then, so nothing downstream noticed
// that the user had given up.
func TestAuthLoginInterruptedAtPromptSavesNothing(t *testing.T) {
	tests := []struct {
		name    string
		seed    *config.File
		answers []string
		// wantAPICall is false only where the interrupt has to stop the
		// command before it verifies the token.
		wantAPICall bool
	}{
		{name: "the token prompt"},
		{name: "the profile name prompt", answers: []string{"xoxp-abc\n"}, wantAPICall: true},
		{
			name:        "the overwrite confirmation for the same workspace",
			wantAPICall: true,
			seed: &config.File{
				DefaultProfile: "myws",
				Profiles: map[string]config.Profile{
					"myws": {Token: "xoxp-old", Host: "myws.slack.com", TeamID: "T1"},
				},
			},
			answers: []string{"xoxp-new\n"},
		},
		{
			name:        "the overwrite confirmation for a different workspace",
			wantAPICall: true,
			seed: &config.File{
				DefaultProfile: "taken",
				Profiles: map[string]config.Profile{
					"taken": {Token: "xoxp-other", Host: "other.slack.com", TeamID: "T2"},
				},
			},
			answers: []string{"xoxp-new\n", "taken\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if tt.seed != nil {
				if err := tt.seed.Save(); err != nil {
					t.Fatalf("seed config Save() error = %v", err)
				}
			}
			srv := newAuthTestServer(t, "myws.slack.com", "T1")
			called := stubSlackClientFactory(t, srv)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			stderr, exit, err := runAuthLoginWithContext(t, ctx,
				newInterruptedStdin(t, cancel, tt.answers...))
			assertInterrupted(t, stderr, exit, err)
			if called.Load() != tt.wantAPICall {
				t.Errorf("slackClientFactory called = %v, want %v", called.Load(), tt.wantAPICall)
			}
			// A cancelled prompt ends its own line, so Execute's error
			// message starts on a fresh one instead of continuing it.
			if !strings.HasSuffix(stderr, "\n") {
				t.Errorf("stderr = %q, want the cancelled prompt to end its line", stderr)
			}
			assertConfigUnchanged(t, tt.seed)
		})
	}
}

// TestAuthLoginInterruptedWithFinalAnswerSavesNothing covers a signal that
// lands in the window between the last prompt and the write. Which side
// stops the run is not fixed — readLine's select sees the answer and the
// cancellation ready at once and picks at random — which is why
// runAuthLogin checks the context again before saving: without that guard,
// the runs where the read wins would write the profile.
func TestAuthLoginInterruptedWithFinalAnswerSavesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stderr, exit, err := runAuthLoginWithContext(t, ctx,
		newStdinInterruptedWithLastAnswer(t, cancel, "xoxp-abc\n", "myws\n"))
	assertInterrupted(t, stderr, exit, err)
	assertConfigUnchanged(t, nil)
}

// TestAuthLoginSignalAtPromptAborts drives the real signals rather than a
// bare context cancellation, so SIGTERM is covered alongside SIGINT.
func TestAuthLoginSignalAtPromptAborts(t *testing.T) {
	// Pinning the list rather than just iterating it: iterating alone would
	// still pass if Execute stopped registering SIGTERM.
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if got := interruptSignals(); !slices.Equal(got, signals) {
		t.Fatalf("interruptSignals() = %v, want %v", got, signals)
	}

	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("os.FindProcess() error = %v", err)
	}

	for _, sig := range signals {
		t.Run(sig.String(), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			srv := newAuthTestServer(t, "myws.slack.com", "T1")
			stubSlackClientFactory(t, srv)

			// The same registration Execute makes, so the signal is
			// handled here instead of terminating the test binary.
			ctx, stop := signal.NotifyContext(context.Background(), sig)
			defer stop()

			stdin := newInterruptedStdin(t, func() {
				if err := self.Signal(sig); err != nil {
					t.Errorf("sending %v to the test process: %v", sig, err)
				}
			}, "xoxp-abc\n")

			stderr, exit, err := runAuthLoginWithContext(t, ctx, stdin)
			assertInterrupted(t, stderr, exit, err)
			assertConfigUnchanged(t, nil)
		})
	}
}
