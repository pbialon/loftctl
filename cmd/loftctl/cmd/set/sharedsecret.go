package set

import (
	"context"
	"github.com/loft-sh/loftctl/cmd/loftctl/flags"
	"github.com/loft-sh/loftctl/pkg/client"
	"github.com/loft-sh/loftctl/pkg/log"
	"github.com/loft-sh/loftctl/pkg/survey"
	"github.com/loft-sh/loftctl/pkg/upgrade"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

// SharedSecretCmd holds the lags
type SharedSecretCmd struct {
	*flags.GlobalFlags

	log log.Logger
}

// NewSharedSecretCmd creates a new command
func NewSharedSecretCmd(globalFlags *flags.GlobalFlags) *cobra.Command {
	cmd := &SharedSecretCmd{
		GlobalFlags: globalFlags,
		log:         log.GetInstance(),
	}
	description := `
#######################################################
################### loft set secret ###################
#######################################################
Sets the key value of a shared secret

Example:
loft set secret test-secret.key value
#######################################################
	`
	if upgrade.IsPlugin == "true" {
		description = `
#######################################################
################# devspace set secret #################
#######################################################
Sets the key value of a shared secret

Example:
devspace set secret test-secret.key value
#######################################################
	`
	}
	c := &cobra.Command{
		Use:   "secret [secret.key] [value]",
		Short: "Sets the key value of a shared secret",
		Long:  description,
		Args:  cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return cmd.Run(cobraCmd, args)
		},
	}

	return c
}

// RunUsers executes the functionality
func (cmd *SharedSecretCmd) Run(cobraCmd *cobra.Command, args []string) error {
	baseClient, err := client.NewClientFromPath(cmd.Config)
	if err != nil {
		return err
	}

	managementClient, err := baseClient.Management()
	if err != nil {
		return err
	}

	// get secret
	secretName := ""
	keyName := ""
	secretArg := args[0]
	idx := strings.Index(secretArg, ".")
	if idx == -1 {
		secretName = secretArg
	} else {
		secretName = secretArg[:idx]
		keyName = secretArg[idx+1:]
	}

	secret, err := managementClient.Loft().ManagementV1().SharedSecrets().Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "get secret")
	}

	if keyName == "" {
		if len(secret.Spec.Data) == 0 {
			return errors.Errorf("secret %s has no keys. Please specify a key like `loft set secret name.key value`", secretName)
		}

		keyNames := []string{}
		for k := range secret.Spec.Data {
			keyNames = append(keyNames, k)
		}

		keyName, err = cmd.log.Question(&survey.QuestionOptions{
			Question:     "Please select a secret key to set",
			DefaultValue: keyNames[0],
			Options:      keyNames,
		})
		if err != nil {
			return errors.Wrap(err, "ask question")
		}
	}

	// Update the secret
	if secret.Spec.Data == nil {
		secret.Spec.Data = map[string][]byte{}
	}
	secret.Spec.Data[keyName] = []byte(args[1])
	_, err = managementClient.Loft().ManagementV1().SharedSecrets().Update(context.TODO(), secret, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "update secret")
	}

	cmd.log.Donef("Successfully set secret key %s.%s", secretName, keyName)
	return nil
}
