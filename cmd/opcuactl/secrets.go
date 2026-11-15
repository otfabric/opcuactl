// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// A secret (a password) comes from a file, from the environment or from a
// flag. A flag value is visible in the process list and the shell history;
// a file (or stdin) and an environment variable are not.
//
//	--<name>          the value
//	--<name>-file     a file holding it, - for stdin
//	<ENV>             the value, when neither flag is given
//	<ENV>_FILE        a file holding it, when <ENV> is not set either

// maxSecretSize bounds a secret read from a file.
const maxSecretSize = 64 << 10

// readSecret reads a secret from file, or from stdin for "-". The line end
// at its end is not part of it.
func readSecret(file string) (string, error) {
	r := io.Reader(os.Stdin)
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxSecretSize+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxSecretSize {
		return "", fmt.Errorf("more than %d bytes", maxSecretSize)
	}
	s := strings.TrimSuffix(string(b), "\n")
	return strings.TrimSuffix(s, "\r"), nil
}

// resolveSecret sets *value from the flags name and name-file of flags, or
// from the environment variables env and env_FILE. The command line wins
// over the environment; both flags at once are a usage error, a file that
// cannot be read is exit 3. Messages name the flag or variable, never the
// secret.
func resolveSecret(flags *pflag.FlagSet, name, env string, value *string) error {
	fileFlag := name + "-file"
	file, _ := flags.GetString(fileFlag)
	source := "--" + fileFlag
	switch {
	case flags.Changed(name) && flags.Changed(fileFlag):
		return exitCodef(exitUsage, "--%s and --%s: give one of them", name, fileFlag)
	case flags.Changed(name):
		return nil
	case flags.Changed(fileFlag):
	case os.Getenv(env) != "":
		*value = os.Getenv(env)
		return nil
	case os.Getenv(env+"_FILE") != "":
		file, source = os.Getenv(env+"_FILE"), env+"_FILE"
	default:
		return nil
	}
	s, err := readSecret(file)
	if err != nil {
		return exitCodef(exitInvalidInput, "%s: %v", source, err)
	}
	*value = s
	return nil
}
