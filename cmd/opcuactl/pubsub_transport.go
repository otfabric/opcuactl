// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/transport/eth"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/pflag"
)

var pubsubMQTTVersionValues = []string{"3.1.1", "5.0", "BestAvailable"}

// Transport schemes as the CLI names them in messages.
const pubsubSchemes = "opc.udp://, opc.eth://, mqtt://, mqtts://, amqp:// or amqps://"

// isBrokerScheme tells the broker transports (MQTT, AMQP) from the datagram
// transports (UDP, Ethernet).
func isBrokerScheme(scheme string) bool {
	switch scheme {
	case "mqtt", "mqtts", "amqp", "amqps":
		return true
	}
	return false
}

func isMQTTScheme(scheme string) bool { return scheme == "mqtt" || scheme == "mqtts" }

// transportProfile returns the TransportProfileUri for a URL and mapping.
func transportProfile(url string, enc parse.PubSubEncoding) (string, error) {
	scheme, err := transport.Scheme(url)
	if err != nil {
		return "", err
	}
	json := enc == parse.EncodingJSON
	switch scheme {
	case "opc.udp":
		if json {
			return "", errors.New("the JSON mapping is not available over UDP: use uadp, or a broker (MQTT, AMQP)")
		}
		return transport.ProfileUDPUADP, nil
	case "opc.eth":
		if json {
			return "", errors.New("the JSON mapping is not available over Ethernet: use uadp, or a broker (MQTT, AMQP)")
		}
		return transport.ProfileEthUADP, nil
	case "mqtt", "mqtts":
		if json {
			return transport.ProfileMQTTJSON, nil
		}
		return transport.ProfileMQTTUADP, nil
	case "amqp", "amqps":
		if json {
			return transport.ProfileAMQPJSON, nil
		}
		return transport.ProfileAMQPUADP, nil
	}
	return "", fmt.Errorf("unsupported transport %q (opc.udp, opc.eth, mqtt, mqtts, amqp, amqps)", scheme)
}

// pubsubTransportFlags configure the transport connection.
type pubsubTransportFlags struct {
	url         string
	iface       string
	username    string
	password    string
	clientID    string
	mqttVersion string
	qos         uint8
	properties  []string
}

func (f *pubsubTransportFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.url, "url", "", "Transport address: opc.udp://host:port (unicast or multicast group), opc.eth://MAC[:VID[.PCP]], mqtt(s)://host:port or amqp(s)://host:port")
	fs.StringVar(&f.iface, "interface", "", "Network interface: for UDP multicast (default: system choice); required for opc.eth://")
	fs.StringVar(&f.username, "broker-username", "", "Broker (MQTT, AMQP) username")
	fs.StringVar(&f.password, "broker-password", "", "Broker (MQTT, AMQP) password (or OPCUACTL_BROKER_PASSWORD)")
	fs.StringVar(&f.clientID, "broker-client-id", "", "MQTT client id / AMQP container-id (default: random)")
	// The flags of v0.4, kept for scripts.
	fs.StringVar(&f.username, "mqtt-username", "", "MQTT broker username")
	fs.StringVar(&f.password, "mqtt-password", "", "MQTT broker password")
	fs.StringVar(&f.clientID, "mqtt-client-id", "", "MQTT client id")
	_ = fs.MarkDeprecated("mqtt-username", "use --broker-username")
	_ = fs.MarkDeprecated("mqtt-password", "use --broker-password")
	_ = fs.MarkDeprecated("mqtt-client-id", "use --broker-client-id")
	fs.StringVar(&f.mqttVersion, "mqtt-version", "", "MQTT protocol version (3.1.1|5.0|BestAvailable; default 3.1.1)")
	fs.Uint8Var(&f.qos, "qos", 0, "Broker delivery guarantee: 0 at most once, 1 at least once, 2 exactly once (AMQP: as 1)")
	fs.StringArrayVar(&f.properties, "transport-property", nil, "Connection property key=value (repeatable): AMQP source-capabilities, connection-hostname, ...; MQTT 5.0 connection-* properties")
}

// config validates the flags and returns the transport configuration.
func (f *pubsubTransportFlags) config(enc parse.PubSubEncoding) (transport.Config, error) {
	var cfg transport.Config
	if f.url == "" {
		return cfg, exitCodef(1, "--url is required (%s)", pubsubSchemes)
	}
	if f.qos > 2 {
		return cfg, exitCodef(1, "--qos must be 0, 1 or 2")
	}
	profile, err := transportProfile(f.url, enc)
	if err != nil {
		return cfg, exitCode(1, err)
	}
	scheme, _ := transport.Scheme(f.url)
	if scheme == "opc.eth" && f.iface == "" {
		return cfg, exitCodef(1, "--interface is required with opc.eth:// (the network interface the frames are sent and received on, e.g. eth0)")
	}
	var version string
	if f.mqttVersion != "" {
		if !isMQTTScheme(scheme) {
			return cfg, exitCodef(1, "--mqtt-version needs an MQTT broker (mqtt://, mqtts://)")
		}
		if version, err = parse.MQTTVersion(f.mqttVersion); err != nil {
			return cfg, exitCode(1, err)
		}
	}
	props, err := parse.TransportProperties(f.properties)
	if err != nil {
		return cfg, exitCode(1, err)
	}
	password := f.password
	for _, env := range []string{"OPCUACTL_BROKER_PASSWORD", "OPCUACTL_MQTT_PASSWORD"} {
		if password == "" {
			password = os.Getenv(env)
		}
	}
	return transport.Config{
		ProfileURI:  profile,
		URL:         f.url,
		Interface:   f.iface,
		MQTTVersion: version,
		ClientID:    f.clientID,
		Username:    f.username,
		Password:    password,
		Properties:  props,
	}, nil
}

func (f *pubsubTransportFlags) open(ctx context.Context, enc parse.PubSubEncoding) (transport.Connection, error) {
	cfg, err := f.config(enc)
	if err != nil {
		return nil, err
	}
	openCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()
	c, err := transport.Open(openCtx, cfg)
	if err != nil {
		return nil, exitCode(2, transportError(f.url, err))
	}
	return c, nil
}

// subscribe subscribes to topic on c, explaining raw socket errors.
func (f *pubsubTransportFlags) subscribe(ctx context.Context, c transport.Connection, topic string, h transport.Handler) (func() error, error) {
	cancel, err := c.Subscribe(ctx, topic, f.qos, h)
	if err != nil {
		return nil, exitCodef(2, "subscribing to %s: %v", f.url, transportError(f.url, err))
	}
	return cancel, nil
}

// transportError adds a hint to the errors of raw Ethernet sockets.
func transportError(url string, err error) error {
	if scheme, _ := transport.Scheme(url); scheme != "opc.eth" {
		return err
	}
	switch {
	case errors.Is(err, eth.ErrNotSupported):
		return fmt.Errorf("%w (opc.eth:// needs Linux, macOS or FreeBSD)", err)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w; run as root, or on Linux grant the capability: sudo setcap cap_net_raw+ep \"$(command -v opcuactl)\"", err)
	}
	return err
}
