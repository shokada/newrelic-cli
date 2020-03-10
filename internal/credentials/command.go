package credentials

import (
	"github.com/fatih/color"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	// Display keys when printing output
	showKeys    bool
	profileName string
	region      string
	apiKey      string
)

// Command is the base command for managing profiles
var Command = &cobra.Command{
	Use:   "profiles",
	Short: "Manage the credential profiles for this tool",
}

var cmdAdd = &cobra.Command{
	Use:   "add",
	Short: "Add a new credential profile",
	Long: `Add a new credential profile

The add command creates a new credential profile for use with the New Relic CLI.
`,
	Example: "newrelic credentials add -n <profileName> -r <region> --apiKey <apiKey>",
	Run: func(cmd *cobra.Command, args []string) {
		WithCredentials(func(creds *Credentials) {
			err := creds.AddProfile(profileName, region, apiKey)
			if err != nil {
				log.Fatal(err)
			}

			cyan := color.New(color.FgCyan).SprintfFunc()
			log.Infof("profile %s added", cyan(profileName))

			if len(creds.Profiles) == 1 {
				err := creds.SetDefaultProfile(profileName)
				if err != nil {
					log.Fatal(err)
				}

				cyan := color.New(color.FgCyan).SprintfFunc()
				log.Infof("setting %s as default profile", cyan(profileName))
			}
		})
	},
}

var cmdDefault = &cobra.Command{
	Use:   "default",
	Short: "Set the default credential profile name",
	Long: `Set the default credential profile name

The default command sets the profile to use by default using the specified name.
`,
	Example: "newrelic credentials default -n <profileName>",
	Run: func(cmd *cobra.Command, args []string) {
		WithCredentials(func(creds *Credentials) {
			err := creds.SetDefaultProfile(profileName)
			if err != nil {
				log.Fatal(err)
			}

			log.Info("success")
		})
	},
}

var cmdList = &cobra.Command{
	Use:   "list",
	Short: "List the credential profiles available",
	Long: `List the credential profiles available

The list command prints out the available profiles' credentials.
`,
	Example: "newrelic credentials list",
	Run: func(cmd *cobra.Command, args []string) {
		WithCredentials(func(creds *Credentials) {
			if creds != nil {
				creds.List()
			} else {
				log.Info("no profiles found")
			}
		})
	},
	Aliases: []string{
		"ls",
	},
}

var cmdRemove = &cobra.Command{
	Use:   "remove",
	Short: "Remove a credential profile",
	Long: `Remove a credential profiles

The remove command removes a credential profile specified by name.
`,
	Example: "newrelic credentials remove -n <profileName>",
	Run: func(cmd *cobra.Command, args []string) {
		WithCredentials(func(creds *Credentials) {
			err := creds.RemoveProfile(profileName)
			if err != nil {
				log.Fatal(err)
			}

			log.Info("success")
		})
	},
	Aliases: []string{
		"rm",
	},
}

func init() {
	var err error

	// Add
	Command.AddCommand(cmdAdd)
	cmdAdd.Flags().StringVarP(&profileName, "profileName", "n", "", "the profile name to add")
	cmdAdd.Flags().StringVarP(&region, "region", "r", "", "the US or EU region")
	cmdAdd.Flags().StringVarP(&apiKey, "apiKey", "", "", "your personal API key")
	err = cmdAdd.MarkFlagRequired("profileName")
	if err != nil {
		log.Error(err)
	}

	err = cmdAdd.MarkFlagRequired("region")
	if err != nil {
		log.Error(err)
	}

	err = cmdAdd.MarkFlagRequired("apiKey")
	if err != nil {
		log.Error(err)
	}

	// Default
	Command.AddCommand(cmdDefault)
	cmdDefault.Flags().StringVarP(&profileName, "profileName", "n", "", "the profile name to set as default")
	err = cmdDefault.MarkFlagRequired("profileName")
	if err != nil {
		log.Error(err)
	}

	// List
	Command.AddCommand(cmdList)
	cmdList.Flags().BoolVarP(&showKeys, "show-keys", "s", false, "list the profiles on your keychain")

	// Remove
	Command.AddCommand(cmdRemove)
	cmdRemove.Flags().StringVarP(&profileName, "profileName", "n", "", "the profile name to remove")
	err = cmdRemove.MarkFlagRequired("profileName")
	if err != nil {
		log.Error(err)
	}
}
