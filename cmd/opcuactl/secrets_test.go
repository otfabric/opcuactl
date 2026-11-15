// SPDX-License-Identifier: MIT

package opcuactl

import (
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
	"github.com/otfabric/opcuactl/internal/serve"
	"github.com/spf13/pflag"
)

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// withStdin runs fn with stdin reading content.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString(content)
		_ = w.Close()
	}()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig; _ = r.Close() }()
	fn()
}

func TestReadSecret(t *testing.T) {
	for _, c := range []struct{ content, want string }{
		{"", ""},
		{"pw", "pw"},
		{"pw\n", "pw"},
		{"pw\r\n", "pw"},
		{"pw\n\n", "pw\n"},
		{" pw with spaces \n", " pw with spaces "},
		{strings.Repeat("x", maxSecretSize), strings.Repeat("x", maxSecretSize)},
	} {
		got, err := readSecret(writeSecretFile(t, c.content))
		if err != nil || got != c.want {
			t.Errorf("%q: %q, %v", c.content, got, err)
		}
	}
	if _, err := readSecret(writeSecretFile(t, strings.Repeat("x", maxSecretSize+1))); err == nil {
		t.Error("a file larger than maxSecretSize is read")
	}
	withStdin(t, "from stdin\n", func() {
		if got, err := readSecret("-"); err != nil || got != "from stdin" {
			t.Errorf("stdin: %q, %v", got, err)
		}
	})
}

// The sources of a secret, and which one wins.
func TestResolveSecret(t *testing.T) {
	file := writeSecretFile(t, "from-file\n")
	envFile := writeSecretFile(t, "from-env-file\n")
	missing := filepath.Join(t.TempDir(), "missing.txt")
	for _, c := range []struct {
		name    string
		args    []string
		env     map[string]string
		want    string
		code    int
		message string
	}{
		{"nothing", nil, nil, "", exitOK, ""},
		{"flag", []string{"--password", "from-flag"}, nil, "from-flag", exitOK, ""},
		{"file", []string{"--password-file", file}, nil, "from-file", exitOK, ""},
		{"environment", nil, map[string]string{"OPCUACTL_PASSWORD": "from-env"}, "from-env", exitOK, ""},
		{"environment file", nil, map[string]string{"OPCUACTL_PASSWORD_FILE": envFile}, "from-env-file", exitOK, ""},
		{"environment before its file", nil, map[string]string{"OPCUACTL_PASSWORD": "from-env", "OPCUACTL_PASSWORD_FILE": envFile}, "from-env", exitOK, ""},
		{"flag before environment", []string{"--password", "from-flag"}, map[string]string{"OPCUACTL_PASSWORD": "from-env"}, "from-flag", exitOK, ""},
		{"file before environment", []string{"--password-file", file}, map[string]string{"OPCUACTL_PASSWORD": "from-env"}, "from-file", exitOK, ""},
		{"both flags", []string{"--password", "x", "--password-file", file}, nil, "", exitUsage, "--password and --password-file: give one of them"},
		{"missing file", []string{"--password-file", missing}, nil, "", exitInvalidInput, "--password-file: open " + missing},
		{"missing environment file", nil, map[string]string{"OPCUACTL_PASSWORD_FILE": missing}, "", exitInvalidInput, "OPCUACTL_PASSWORD_FILE: open " + missing},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"OPCUACTL_PASSWORD", "OPCUACTL_PASSWORD_FILE"} {
				t.Setenv(k, c.env[k])
			}
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			var value string
			fs.StringVar(&value, "password", "", "")
			fs.String("password-file", "", "")
			if err := fs.Parse(c.args); err != nil {
				t.Fatal(err)
			}
			err := resolveSecret(fs, "password", "OPCUACTL_PASSWORD", &value)
			if got := ExitCode(err); got != c.code {
				t.Fatalf("exit %d, want %d (%v)", got, c.code, err)
			}
			if err != nil && !strings.Contains(err.Error(), c.message) {
				t.Errorf("error %q, want %q", err, c.message)
			}
			if err == nil && value != c.want {
				t.Errorf("value %q, want %q", value, c.want)
			}
		})
	}
}

// The password of --auth username from a file, stdin or the environment
// reaches the server: the right one logs in, a wrong one is exit 9.
func TestPasswordSources(t *testing.T) {
	certOut := filepath.Join(t.TempDir(), "server.pem")
	s := startServe(t, "--security-policy", "Basic256Sha256", "--cert-out", certOut, "--hostname", "127.0.0.1",
		"--log-level", "off", "--user", "operator:secret")
	cli := newIdentity(t, "rsa", clientURI)
	args := []string{"get", "value", "-e", s.url(t, ua.TransportProfileUATCP), "-n", s.node(serve.DemoInt32), "-q",
		"--security-policy", "Basic256Sha256", "--security-mode", "SignAndEncrypt",
		"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", certOut, "--auth", "username", "--username", "operator"}
	right, wrong := writeSecretFile(t, "secret\n"), writeSecretFile(t, "wrong\n")
	for _, c := range []struct {
		name  string
		extra []string
		env   map[string]string
		stdin string
		code  int
	}{
		{"file", []string{"--password-file", right}, nil, "", exitOK},
		{"wrong file", []string{"--password-file", wrong}, nil, "", exitPermission},
		{"stdin", []string{"--password-file", "-"}, nil, "secret\n", exitOK},
		{"environment", nil, map[string]string{"OPCUACTL_PASSWORD": "secret"}, "", exitOK},
		{"environment file", nil, map[string]string{"OPCUACTL_PASSWORD_FILE": right}, "", exitOK},
		{"wrong environment", nil, map[string]string{"OPCUACTL_PASSWORD": "wrong"}, "", exitPermission},
		{"flag", []string{"--password", "secret"}, nil, "", exitOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"OPCUACTL_PASSWORD", "OPCUACTL_PASSWORD_FILE"} {
				t.Setenv(k, c.env[k])
			}
			var out, stderr string
			var err error
			withStdin(t, c.stdin, func() { out, stderr, err = runCLI(t, append(args, c.extra...)...) })
			if got := ExitCode(err); got != c.code {
				t.Fatalf("exit %d, want %d (%v)\n%s", got, c.code, err, stderr)
			}
			if c.code == exitOK && strings.TrimSpace(out) != "42" {
				t.Errorf("stdout %q", out)
			}
			if strings.Contains(stderr, "secret") {
				t.Errorf("stderr names the password: %q", stderr)
			}
		})
	}
}

// The password of a PFX key from a file or the environment opens the PKCS
// #12 --private-key before anything is sent.
func TestPFXPasswordSources(t *testing.T) {
	id := newIdentity(t, "rsa", clientURI)
	pfx, err := uapolicy.EncodePFX(id.key.(*rsa.PrivateKey), [][]byte{id.der}, "pfx-secret")
	if err != nil {
		t.Fatal(err)
	}
	pfxFile := filepath.Join(t.TempDir(), "key.p12")
	if err := os.WriteFile(pfxFile, pfx, 0o600); err != nil {
		t.Fatal(err)
	}
	right, wrong := writeSecretFile(t, "pfx-secret\n"), writeSecretFile(t, "nope\n")
	args := []string{"gds", "update-cert", id.certFile, "--private-key", pfxFile, "-e", deadEndpoint, "--timeout", "2s", "--yes"}
	for _, c := range []struct {
		name    string
		extra   []string
		env     string
		code    int
		message string
	}{
		// The key opens, then the endpoint cannot be reached.
		{"file", []string{"--pfx-password-file", right}, "", exitTransport, ""},
		{"environment", nil, "pfx-secret", exitTransport, ""},
		{"wrong file", []string{"--pfx-password-file", wrong}, "", exitUsage, "--pfx-password"},
		{"wrong environment", nil, "nope", exitUsage, "--pfx-password"},
		{"both flags", []string{"--pfx-password", "x", "--pfx-password-file", right}, "", exitUsage, "give one of them"},
		{"missing file", []string{"--pfx-password-file", filepath.Join(t.TempDir(), "missing")}, "", exitInvalidInput, "--pfx-password-file: open"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OPCUACTL_PFX_PASSWORD", c.env)
			_, stderr, err := runCLI(t, append(args, c.extra...)...)
			if got := ExitCode(err); got != c.code || !strings.Contains(stderr, c.message) {
				t.Fatalf("exit %d, want %d (%v)\n%s", got, c.code, err, stderr)
			}
		})
	}
	// Without --private-key, the variable is not used.
	t.Setenv("OPCUACTL_PFX_PASSWORD", "pfx-secret")
	if _, _, err := runCLI(t, "gds", "update-cert", id.certFile, "-e", deadEndpoint, "--timeout", "2s", "--yes"); ExitCode(err) != exitTransport {
		t.Errorf("OPCUACTL_PFX_PASSWORD without --private-key: %v", err)
	}
}

// --key-password-file and OPCUACTL_KEY_PASSWORD are for a key pair the GDS
// generates.
func TestKeyPasswordSources(t *testing.T) {
	csr := filepath.Join(t.TempDir(), "missing.csr")
	base := []string{"gds", "pull", "request", "ns=1;i=1", "-e", deadEndpoint, "--timeout", "2s"}
	for _, c := range []struct {
		name    string
		extra   []string
		env     string
		code    int
		message string
	}{
		{"file with --csr", []string{"--key-password-file", writeSecretFile(t, "k"), "--private-key", newIdentity(t, "rsa", clientURI).keyFile}, "", exitUsage, "--key-password is for a key pair"},
		{"missing file", []string{"--key-password-file", csr}, "", exitInvalidInput, "--key-password-file: open"},
		{"file", []string{"--key-password-file", writeSecretFile(t, "k")}, "", exitTransport, ""},
		{"environment", nil, "k", exitTransport, ""},
		{"environment with --private-key", []string{"--private-key", newIdentity(t, "rsa", clientURI).keyFile}, "k", exitTransport, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OPCUACTL_KEY_PASSWORD", c.env)
			_, stderr, err := runCLI(t, append(base, c.extra...)...)
			if got := ExitCode(err); got != c.code || !strings.Contains(stderr, c.message) {
				t.Fatalf("exit %d, want %d (%v)\n%s", got, c.code, err, stderr)
			}
		})
	}
}

// An invalid serve --user is named by its position: the message holds no
// password.
func TestServeUserErrorHidesPassword(t *testing.T) {
	for _, users := range [][]string{{"--user", ":hunter2"}, {"--user", "ok:pw", "--user", "nocolon-hunter2"}} {
		_, stderr, err := runCLI(t, append([]string{"serve", "--listen", "127.0.0.1:0", "--security-policy", "Basic256Sha256"}, users...)...)
		if ExitCode(err) != exitUsage || strings.Contains(stderr, "hunter2") || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%v: exit %d, %v\n%s", users, ExitCode(err), err, stderr)
		}
		if !strings.Contains(err.Error(), "invalid --user value") {
			t.Errorf("%v: %v", users, err)
		}
	}
	t.Setenv("OPCUACTL_SERVE_USERS", "ok:pw,:hunter2")
	_, stderr, err := runCLI(t, "serve", "--listen", "127.0.0.1:0", "--security-policy", "Basic256Sha256")
	if ExitCode(err) != exitUsage || strings.Contains(stderr, "hunter2") || !strings.Contains(err.Error(), "OPCUACTL_SERVE_USERS value 2") {
		t.Errorf("environment: exit %d, %v\n%s", ExitCode(err), err, stderr)
	}
}
