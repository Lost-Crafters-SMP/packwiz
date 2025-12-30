package maven

import (
	"github.com/packwiz/packwiz/cmd"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

var mavenCmd = &cobra.Command{
	Use:     "maven",
	Aliases: []string{"mv", "mvn"},
	Short:   "Manage artifacts from Maven repositories",
}

func init() {
	cmd.Add(mavenCmd)
	core.Updaters["maven"] = mavenUpdater{}
}

