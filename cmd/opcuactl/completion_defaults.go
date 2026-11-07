// SPDX-License-Identifier: MIT

package opcuactl

import (
	"net"
	"slices"
	"strings"

	"github.com/otfabric/go-opcua/ua"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Completion for the flags and arguments the commands do not complete
// themselves. registerDefaultCompletions runs once over the whole tree, after
// every command registered its own; it never replaces one.

var (
	certFileExts     = []string{"pem", "der", "crt", "cer"}
	certOrCRLExts    = []string{"pem", "der", "crt", "cer", "crl"}
	keyFileExts      = []string{"pem", "der", "key"}
	privateKeyExts   = []string{"pem", "der", "key", "p12", "pfx"}
	endpointSchemes  = []string{"opc.tcp://", "opc.wss://", "opc.https://"}
	pubsubURLSchemes = []string{"opc.udp://", "opc.eth://", "mqtt://", "mqtts://", "amqp://", "amqps://"}
	appTypeValues    = []string{"server", "client", "clientandserver"}
	qosValues        = []string{"0", "1", "2"}
)

// fileFlags are the flags that name a file of a known type.
var fileFlags = map[string][]string{
	"cert":         certFileExts,
	"server-cert":  certFileExts,
	"trusted-cert": certFileExts,
	"tls-ca":       certFileExts,
	"tls-cert":     certFileExts,
	"csr":          {"pem", "der", "csr"},
	"issuer":       certOrCRLExts,
	"key":          keyFileExts,
	"tls-key":      keyFileExts,
	"private-key":  privateKeyExts,
	"arg-file":     {"json"},
	"events-file":  {"json"},
	"keys-file":    {"json"},
	"metadata":     {"json"},
	"nodeset":      {"xml"},
}

// anyFileFlags name a file of any type: an output file, a token, a list.
var anyFileFlags = map[string]bool{
	"output": true, "cert-out": true, "key-out": true, "issuers-out": true,
	"token-file": true, "nodes-file": true,
}

// dirFlags name a directory.
var dirFlags = map[string]bool{"trust-dir": true, "trust-out": true}

func registerDefaultCompletions(c *cobra.Command) {
	if c.Runnable() && c.ValidArgs == nil && c.ValidArgsFunction == nil {
		c.ValidArgsFunction = defaultArgsCompletion(c.Use)
	}
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) { registerDefaultFlagCompletion(c, f) })
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) { registerDefaultFlagCompletion(c, f) })
	for _, sub := range c.Commands() {
		registerDefaultCompletions(sub)
	}
}

// defaultArgsCompletion completes certificate files for a command whose
// arguments are certificates, files for one that reads files, and nothing
// for the others (node IDs, application IDs, or no arguments at all).
func defaultArgsCompletion(use string) cobra.CompletionFunc {
	switch {
	case strings.Contains(use, "cert>"):
		return cobra.FixedCompletions(certOrCRLExts, cobra.ShellCompDirectiveFilterFileExt)
	case strings.Contains(use, "[file"):
		return cobra.FixedCompletions(nil, cobra.ShellCompDirectiveDefault)
	default:
		return cobra.NoFileCompletions
	}
}

func registerDefaultFlagCompletion(c *cobra.Command, f *pflag.Flag) {
	if f.Value.Type() == "bool" {
		return
	}
	if _, ok := c.GetFlagCompletionFunc(f.Name); ok {
		return
	}
	_ = c.RegisterFlagCompletionFunc(f.Name, defaultFlagCompletion(f))
}

func defaultFlagCompletion(f *pflag.Flag) cobra.CompletionFunc {
	switch name := f.Name; {
	case fileFlags[name] != nil:
		return cobra.FixedCompletions(fileFlags[name], cobra.ShellCompDirectiveFilterFileExt)
	case anyFileFlags[name]:
		return cobra.FixedCompletions(nil, cobra.ShellCompDirectiveDefault)
	case dirFlags[name], name == "out" && strings.Contains(f.Usage, "directory"):
		return cobra.FixedCompletions(nil, cobra.ShellCompDirectiveFilterDirs)
	case name == "out":
		return cobra.FixedCompletions(nil, cobra.ShellCompDirectiveDefault)
	case name == "endpoint":
		return cobra.FixedCompletions(endpointSchemes, cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveNoSpace)
	case name == "url":
		return cobra.FixedCompletions(pubsubURLSchemes, cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveNoSpace)
	case name == "interface":
		return completeInterfaces
	case name == "encoding":
		return fixedCompletion([]string{"uadp", "json"})
	case name == "ua-encoding":
		return fixedCompletion(uaEncodingValues)
	case name == "qos":
		return fixedCompletion(qosValues)
	case name == "capability":
		return completeCapabilities
	case name == "type" && strings.Contains(f.Usage, "server|client"):
		return fixedCompletion(appTypeValues)
	case name == "type" && strings.Contains(f.Usage, "Certificate type"):
		return fixedCompletion(certificateTypeValues())
	case name == "session":
		return fixedCompletion([]string{"self"})
	case name == "event-id" && f.Value.Type() == "string":
		return fixedCompletion([]string{"latest"})
	default:
		return cobra.NoFileCompletions
	}
}

// completeCapabilities offers the server capability identifiers of OPC
// 10000-12 Annex D, with their descriptions.
func completeCapabilities(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	out := make([]string, 0, len(ua.KnownCapabilities))
	for id, desc := range ua.KnownCapabilities {
		out = append(out, id+"\t"+desc)
	}
	slices.Sort(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// netInterfaces lists the network interfaces; a variable so the tests can make it fail.
var netInterfaces = net.Interfaces

func completeInterfaces(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(ifaces))
	for _, i := range ifaces {
		names = append(names, i.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
