package maven

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a default maven.toml configuration file",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		packFile := viper.GetString("pack-file")
		packDir := filepath.Dir(packFile)
		mavenTomlPath := filepath.Join(packDir, "maven.toml")

		// Check if maven.toml already exists
		if _, err := os.Stat(mavenTomlPath); err == nil {
			if !viper.GetBool("maven.init.force") {
				fmt.Println("maven.toml already exists! Use --force to overwrite.")
				os.Exit(1)
			}
		} else if !os.IsNotExist(err) {
			fmt.Printf("Error checking maven.toml: %s\n", err)
			os.Exit(1)
		}

		// Create default config
		config := MavenConfig{
			MavenCentral: MavenCentralConfig{
				Enabled:  true,
				Priority: 1,
			},
			Repositories: []RepositoryConfig{
				{
					Name:     "Fabric Maven",
					URL:      "https://maven.fabricmc.net",
					Priority: 2,
				},
			},
		}

		// Write the file
		file, err := os.Create(mavenTomlPath)
		if err != nil {
			fmt.Printf("Error creating maven.toml: %s\n", err)
			os.Exit(1)
		}
		defer file.Close()

		encoder := toml.NewEncoder(file)
		encoder.Indent = ""
		err = encoder.Encode(config)
		if err != nil {
			fmt.Printf("Error writing maven.toml: %s\n", err)
			os.Exit(1)
		}

		fmt.Printf("Created default maven.toml at %s\n", mavenTomlPath)
		fmt.Println("Edit this file to configure additional Maven repositories.")
	},
}

func init() {
	mavenCmd.AddCommand(initCmd)

	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing maven.toml if it exists")
	_ = viper.BindPFlag("maven.init.force", initCmd.Flags().Lookup("force"))
}

