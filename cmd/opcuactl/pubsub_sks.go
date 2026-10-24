// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/otfabric/go-opcua/pubsub/security"
	"github.com/otfabric/go-opcua/pubsub/sks"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/spf13/cobra"
)

func newPubSubSKSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sks",
		Short: "Security Key Service (SKS) client",
		Long: `Talk to the Security Key Service of a server (Part 14 §8).

An SKS hands out the keys that sign and encrypt the PubSub messages of a
SecurityGroup, only over a SignAndEncrypt session and usually not to
anonymous users.

Examples:
  opcuactl pubsub sks get-keys -e opc.tcp://sks:4840 --security-group sg1 \
      --security-policy Basic256Sha256 --security-mode SignAndEncrypt \
      --cert client.pem --key client.key --auth username --username pub --password secret`,
	}
	cmd.AddCommand(newPubSubSKSGetKeysCmd())
	return cmd
}

func newPubSubSKSGetKeysCmd() *cobra.Command {
	var (
		group         string
		startingToken uint32
		futureKeys    uint32
		showKeys      bool
	)
	cmd := &cobra.Command{
		Use:   "get-keys",
		Short: "Get the keys of a SecurityGroup (GetSecurityKeys)",
		Long: `Call GetSecurityKeys on the Security Key Service at --endpoint and print the
security policy, the token ids and lifetimes of the keys of a SecurityGroup.

The session must use SignAndEncrypt (--security-mode SignAndEncrypt or auto)
and an identity the SKS hands keys to.

Key material is only printed with --show-keys. Save it with
--show-keys --format json to use it with --keys-file of "pubsub subscribe",
"pubsub publish" and "pubsub decode". Treat that file as a secret.

--starting-token 0 asks for the current key; --future-keys is the number of
keys after it (the SKS may return fewer).

Examples:
  opcuactl pubsub sks get-keys -e opc.tcp://sks:4840 --security-group sg1 \
      --security-mode SignAndEncrypt --security-policy Basic256Sha256 \
      --cert client.pem --key client.key --auth username --username pub --password secret
  opcuactl pubsub sks get-keys ... --future-keys 3 --show-keys --format json > keys.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if group == "" {
				return exitCodef(1, "--security-group is required")
			}
			if err := connCfg.RequireEndpoint(); err != nil {
				return exitCode(1, err)
			}
			ctx := context.Background()
			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, cancel := connCfg.TimeoutContext(ctx)
			defer cancel()
			res, err := sks.GetSecurityKeys(opCtx, c, group, startingToken, futureKeys)
			if err != nil {
				return exitCodef(3, "GetSecurityKeys: %v", err)
			}
			out := securityKeysModel(group, res, showKeys)

			if format.ParseFormat(outputFormat) != format.Table {
				return newWriter().Write(out)
			}
			fmt.Printf("SecurityGroup:  %s\n", out.SecurityGroupID)
			fmt.Printf("Policy:         %s\n", out.SecurityPolicyURI)
			fmt.Printf("FirstTokenId:   %d\n", out.FirstTokenID)
			fmt.Printf("TimeToNextKey:  %s\n", out.TimeToNextKey)
			fmt.Printf("KeyLifetime:    %s\n", out.KeyLifetime)
			headers := []string{"TOKEN", "LENGTH"}
			if showKeys {
				headers = append(headers, "KEY")
			}
			rows := make([][]string, len(out.Keys))
			for i, k := range out.Keys {
				rows[i] = []string{fmt.Sprint(k.TokenID), fmt.Sprint(k.Length)}
				if showKeys {
					rows[i] = append(rows[i], k.Key)
				}
			}
			return newWriter().WriteTable(headers, rows)
		},
	}
	cmd.Flags().StringVar(&group, "security-group", "", "SecurityGroupId")
	cmd.Flags().Uint32Var(&startingToken, "starting-token", 0, "First token id wanted (0 = the current key)")
	cmd.Flags().Uint32Var(&futureKeys, "future-keys", 1, "Number of future keys wanted after the first")
	cmd.Flags().BoolVar(&showKeys, "show-keys", false, "Print the key material (base64); treat the output as a secret")
	return cmd
}

func securityKeysModel(group string, res *sks.Result, showKeys bool) model.SecurityKeys {
	out := model.SecurityKeys{
		SecurityGroupID:   group,
		SecurityPolicyURI: res.SecurityPolicyURI,
		FirstTokenID:      res.FirstTokenID,
		TimeToNextKey:     res.TimeToNextKey.String(),
		KeyLifetime:       res.KeyLifetime.String(),
		Keys:              []model.SecurityKey{},
	}
	token := res.FirstTokenID
	for i, k := range res.Keys {
		if i > 0 {
			token = security.NextTokenID(token)
		}
		sk := model.SecurityKey{TokenID: token, Length: len(k)}
		if showKeys {
			sk.Key = base64.StdEncoding.EncodeToString(k)
		}
		out.Keys = append(out.Keys, sk)
	}
	return out
}
