package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/steipete/eightctl/internal/client"
	"github.com/steipete/eightctl/internal/daemon"
)

var tempCmd = &cobra.Command{
	Use:   "temp <value>",
	Short: "Set pod temperature (68F, 20C, or raw API level -100..100)",
	Long: strings.TrimSpace(`
Set pod temperature using either degrees (e.g. 68F, 20C) or the raw Eight Sleep API level range -100..100.

Note: the Eight Sleep app displays a coarser UI scale of roughly -10..10.
In practice, raw API levels appear to map in steps of 10:
raw -10 -> app -1, raw -20 -> app -2, raw 10 -> app +1, raw 20 -> app +2.

Negative raw values must be passed after --, for example: eightctl temp -- -20
`),
	Example: strings.TrimSpace(`
eightctl temp 68F
eightctl temp 20C
eightctl temp 20
eightctl temp -- -20
`),
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuthFields(); err != nil {
			return err
		}
		lvl, err := daemon.ParseTemp(args[0])
		if err != nil {
			return err
		}
		cl := client.New(viper.GetString("email"), viper.GetString("password"), viper.GetString("user_id"), viper.GetString("client_id"), viper.GetString("client_secret"))
		if err := cl.SetTemperature(context.Background(), lvl); err != nil {
			return err
		}
		fmt.Printf("temperature set (level %d)\n", lvl)
		return nil
	},
}
