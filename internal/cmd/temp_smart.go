package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/steipete/eightctl/internal/client"
	"github.com/steipete/eightctl/internal/daemon"
	"github.com/steipete/eightctl/internal/output"
)

var tempSmartCmd = &cobra.Command{
	Use:   "smart",
	Short: "View or update bedtime/night/dawn smart temperatures",
	Long: strings.TrimSpace(`
Manage the app-style smart sleep-stage temperature targets.

Stage mapping:
- bedtime -> bedTimeLevel
- night (alias: early) -> initialSleepLevel
- dawn (alias: late) -> finalSleepLevel

Values accept degrees (e.g. 68F, 20C) or raw API levels -100..100.
Negative raw values must be passed after --, for example:
  eightctl temp smart set dawn -- -20
`),
}

var tempSmartStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show bedtime/night/dawn smart temperatures",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuthFields(); err != nil {
			return err
		}
		cl := client.New(viper.GetString("email"), viper.GetString("password"), viper.GetString("user_id"), viper.GetString("client_id"), viper.GetString("client_secret"))
		st, err := cl.GetSmartTemperatureStatus(context.Background())
		if err != nil {
			return err
		}
		if st.Smart == nil {
			return fmt.Errorf("smart temperature settings not present in response")
		}

		row := map[string]any{
			"mode":          st.CurrentState.Type,
			"phase":         smartTemperaturePhase(st.CurrentState.Type),
			"current_level": st.CurrentLevel,
			"bedtime":       st.Smart.BedTimeLevel,
			"night":         st.Smart.InitialSleepLevel,
			"dawn":          st.Smart.FinalSleepLevel,
		}
		fields := viper.GetStringSlice("fields")
		rows := output.FilterFields([]map[string]any{row}, fields)
		headers := []string{"mode", "phase", "current_level", "bedtime", "night", "dawn"}
		if len(fields) > 0 {
			headers = fields
		}
		return output.Print(output.Format(viper.GetString("output")), headers, rows)
	},
}

var tempSmartSetCmd = &cobra.Command{
	Use:   "set <stage> <value>",
	Short: "Set a bedtime/night/dawn smart temperature",
	Long: strings.TrimSpace(`
Set one smart sleep-stage target. Accepted stage names:
- bedtime
- night (alias: early)
- dawn (alias: late)

Values accept degrees (68F, 20C) or raw API levels -100..100.
`),
	Example: strings.TrimSpace(`
eightctl temp smart set bedtime 68F
eightctl temp smart set night 20
eightctl temp smart set dawn -- -20
`),
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuthFields(); err != nil {
			return err
		}
		stage, err := client.ResolveSmartTemperatureStage(args[0])
		if err != nil {
			return err
		}
		lvl, err := daemon.ParseTemp(args[1])
		if err != nil {
			return err
		}
		cl := client.New(viper.GetString("email"), viper.GetString("password"), viper.GetString("user_id"), viper.GetString("client_id"), viper.GetString("client_secret"))
		if _, err := cl.SetSmartTemperatureLevel(context.Background(), stage, lvl); err != nil {
			return err
		}
		fmt.Printf("%s smart temperature set (level %d)\n", stage, lvl)
		return nil
	},
}

func smartTemperaturePhase(mode string) string {
	switch mode {
	case "smart:bedtime":
		return "bedtime"
	case "smart:initial":
		return "night"
	case "smart:final":
		return "dawn"
	default:
		return ""
	}
}

func init() {
	tempSmartCmd.AddCommand(tempSmartStatusCmd, tempSmartSetCmd)
	tempCmd.AddCommand(tempSmartCmd)
}
