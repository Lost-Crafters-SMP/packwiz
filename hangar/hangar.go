package hangar

import (
	"net/http"

	"github.com/packwiz/packwiz/cmd"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

var hangarCmd = &cobra.Command{
	Use:     "hangar",
	Aliases: []string{"hg"},
	Short:   "Manage Hangar-based plugins",
}

var hgDefaultClient = hangarApiClient{&http.Client{}}

func init() {
	cmd.Add(hangarCmd)
	core.Updaters["hangar"] = hangarUpdater{}
}

