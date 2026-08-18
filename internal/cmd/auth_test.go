package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/slackclient"
	"golang.org/x/term"
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
// API at all. The dead address means a call that should not have happened
// fails instead of panicking, leaving the returned flag to report it to the
// assertions rather than from inside the command.
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

// runAuthLoginForTest drives `auth login` to completion with the given
// stdin and returns what it wrote to stderr. stdout is asserted empty for
// every caller: the command is interactive only, so nothing it prints
// belongs on the stream that carries machine-readable output. stdin is an
// io.Reader rather than a string because the non-TTY case needs a real
// *os.File.
func runAuthLoginForTest(t *testing.T, stdin io.Reader) (string, error) {
	t.Helper()
	root, out, errOut := newTestRoot(t)
	root.SetIn(stdin)
	root.SetArgs([]string{"auth", "login"})

	err := root.Execute()

	if out.Len() > 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	return errOut.String(), err
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

// guardTestFd stands in for the terminal's descriptor. The guard only hands
// it to its restore function, which these tests stub, so no real descriptor
// is touched.
const guardTestFd = 42

// guardEffectTimeout bounds how long a test waits for the guard to act on a
// signal. It only ever fires on a regression, so it is generous enough that
// a loaded CI machine does not trip it.
const guardEffectTimeout = 10 * time.Second

// selfProcess is the handle a guard test sends its signal through. Sending
// a real one is what makes the test exercise the registration the guard
// makes, rather than a channel the test filled itself.
func selfProcess(t *testing.T) *os.Process {
	t.Helper()
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("os.FindProcess() error = %v", err)
	}
	return self
}

// guardRecorder records what the guard did, in order. The guard acts on its
// own goroutine, so every field is read behind the mutex — except reraised,
// which is the channel a test waits on, and whose receive also orders the
// writes that came before it.
type guardRecorder struct {
	mu           sync.Mutex
	events       []string
	resetSignals []os.Signal
	restoreFd    int
	restoreState *term.State

	reraised chan syscall.Signal
}

func (r *guardRecorder) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *guardRecorder) snapshot() (events []string, resetSignals []os.Signal, restoreFd int, restoreState *term.State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events), slices.Clone(r.resetSignals), r.restoreFd, r.restoreState
}

// newRecordingGuard builds a guard whose three process-level effects are
// recorded instead of performed. Stubbing them is what makes the ordering
// observable at all: term.Restore needs a real terminal, and the real
// re-raise ends the process.
func newRecordingGuard(out io.Writer, state *term.State) (*terminalGuard, *guardRecorder) {
	rec := &guardRecorder{reraised: make(chan syscall.Signal, 1)}

	g := newTerminalGuard(out, guardTestFd, state)
	g.reset = func(signals ...os.Signal) {
		rec.mu.Lock()
		rec.resetSignals = signals
		rec.mu.Unlock()
		rec.record("reset")
	}
	g.restore = func(fd int, state *term.State) error {
		rec.mu.Lock()
		rec.restoreFd, rec.restoreState = fd, state
		rec.mu.Unlock()
		rec.record("restore")
		return nil
	}
	g.reraise = func(sig syscall.Signal) {
		rec.record("reraise")
		rec.reraised <- sig
	}
	return g, rec
}

// TestTerminalGuardReRaisesAfterRestoring covers the guard's whole reason to
// exist: a signal arriving while the terminal is modified has to put the
// terminal back before the process dies of that signal. The order matters
// beyond tidiness — resetting the handlers first is what lets a second
// signal end the process at once instead of queueing behind the clean-up.
//
// What the stubs cannot cover — that the real re-raise terminates the
// process, and that echo survives it — is verified by hand against a PTY.
func TestTerminalGuardReRaisesAfterRestoring(t *testing.T) {
	// Pinning the list rather than just iterating it: iterating alone would
	// still pass if the guard stopped registering SIGTERM.
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if got := interruptSignals(); !slices.Equal(got, signals) {
		t.Fatalf("interruptSignals() = %v, want %v", got, signals)
	}

	self := selfProcess(t)

	for _, sig := range signals {
		t.Run(sig.String(), func(t *testing.T) {
			var out bytes.Buffer
			state := &term.State{}
			g, rec := newRecordingGuard(&out, state)

			disarm := g.arm()
			t.Cleanup(disarm)

			if err := self.Signal(sig); err != nil {
				t.Fatalf("sending %v to the test process: %v", sig, err)
			}

			var reraised syscall.Signal
			select {
			case reraised = <-rec.reraised:
			case <-time.After(guardEffectTimeout):
				t.Fatal("the guard did not re-raise the signal")
			}

			if reraised != sig {
				t.Errorf("re-raised %v, want %v", reraised, sig)
			}
			events, resetSignals, restoreFd, restoreState := rec.snapshot()
			if want := []string{"reset", "restore", "reraise"}; !slices.Equal(events, want) {
				t.Errorf("guard did %v, want %v", events, want)
			}
			// Resetting only the signal that arrived would leave the other
			// one delivered to a channel nobody reads any more.
			if !slices.Equal(resetSignals, interruptSignals()) {
				t.Errorf("reset %v, want %v", resetSignals, interruptSignals())
			}
			if restoreFd != guardTestFd || restoreState != state {
				t.Errorf("restored (%d, %p), want (%d, %p)",
					restoreFd, restoreState, guardTestFd, state)
			}
			// Nothing echoes the interrupt with ECHO cleared, so the guard
			// owes the prompt its closing newline — and nothing else.
			if got := out.String(); got != "\n" {
				t.Errorf("guard wrote %q, want a single newline", got)
			}
		})
	}
}

// TestTerminalGuardDisarmStopsDelivery covers the other end of the guard's
// life: once the masked read is over the terminal is unmodified again, and a
// signal has to reach the process default rather than the guard.
func TestTerminalGuardDisarmStopsDelivery(t *testing.T) {
	self := selfProcess(t)

	var out bytes.Buffer
	g, rec := newRecordingGuard(&out, &term.State{})
	disarm := g.arm()

	// Taking delivery over before disarming: with no handler left, the
	// signal below would terminate the test binary rather than prove
	// anything.
	delivered := make(chan os.Signal, 1)
	signal.Notify(delivered, os.Interrupt)
	t.Cleanup(func() { signal.Stop(delivered) })

	disarm()

	if err := self.Signal(os.Interrupt); err != nil {
		t.Fatalf("sending %v to the test process: %v", os.Interrupt, err)
	}
	select {
	case <-delivered:
	case <-time.After(guardEffectTimeout):
		t.Fatal("the signal was never delivered")
	}

	// A goroutine that has returned leaves nothing to observe directly, so
	// the proof is that none of the guard's effects run.
	select {
	case sig := <-rec.reraised:
		t.Errorf("the guard re-raised %v after being disarmed", sig)
	case <-time.After(100 * time.Millisecond):
	}
	if events, _, _, _ := rec.snapshot(); len(events) > 0 {
		t.Errorf("guard did %v after being disarmed, want nothing", events)
	}
	if out.Len() > 0 {
		t.Errorf("guard wrote %q after being disarmed, want nothing", out.String())
	}
}
