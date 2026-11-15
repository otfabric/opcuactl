// SPDX-License-Identifier: MIT

package opcuactl

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/opcuactl/internal/model"
)

// With the keys of a SecurityGroup (--keys-file, --security-group) and no
// --message-security, pubsub subscribe requires Sign: an unsigned UADP
// message is reported on stderr and not printed. --message-security none
// accepts it, as a subscriber without keys does.
func TestPubSubSubscribeMinimumWithKeys(t *testing.T) {
	t.Parallel()
	ctl := startCLIWorker(t)
	key := make([]byte, security.Aes128CTR().KeyLength())
	for i := range key {
		key[i] = byte(i)
	}
	b, err := json.Marshal(model.SecurityKeys{SecurityGroupID: "sg", SecurityPolicyURI: security.PolicyURIAes128CTR,
		FirstTokenID: 1, KeyLifetime: "1h", Keys: []model.SecurityKey{{TokenID: 1, Length: len(key), Key: base64.StdEncoding.EncodeToString(key)}}})
	if err != nil {
		t.Fatal(err)
	}
	keysFile := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(keysFile, b, 0o600); err != nil {
		t.Fatal(err)
	}

	url := freeUDPURL(t)
	startPublisher(t, pubsub.UDPConnection("pub", url, uint16(2234)),
		pubsub.WriterGroup("G", 100, 50*time.Millisecond, pubsub.DataSetWriter("W", 7, "Measurements")))
	subscribe := []string{"pubsub", "subscribe", "--url", url, "--format", "json"}

	for _, refused := range [][]string{
		{"--keys-file", keysFile},
		{"--keys-file", keysFile, "--message-security", "sign"},
	} {
		// The publisher sends every 50 ms: the 2 s see many messages.
		out, stderr, err := ctl.run(t, append(append(subscribe, "--duration", "2s"), refused...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", refused, err, stderr)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("%v: an unsigned message was printed:\n%s", refused, out)
		}
		if !strings.Contains(stderr, "message security MessageSecurityModeNone, required MessageSecurityModeSign") {
			t.Errorf("%v: stderr %q", refused, stderr)
		}
	}

	for _, accepted := range [][]string{
		{"--keys-file", keysFile, "--message-security", "none"},
		nil,
	} {
		out, stderr, err := ctl.run(t, append(append(subscribe, "--count", "1", "--duration", "15s"), accepted...)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", accepted, err, stderr)
		}
		if got := parseDataSets(t, out); len(got) != 1 || got[0].Security != "None" || got[0].DataSetWriterID != 7 {
			t.Errorf("%v: %+v\n%s", accepted, got, stderr)
		}
	}
}
