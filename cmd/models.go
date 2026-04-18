package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/usr-wwelsh/turbolab/internal/hf"
)

var searchLimit int

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage and search HuggingFace models",
}

var modelsSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search HuggingFace for text-generation models",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		models, err := hf.Search(args[0], searchLimit)
		if err != nil {
			return err
		}
		if len(models) == 0 {
			fmt.Println("No models found.")
			return nil
		}
		fmt.Printf("%-50s  %7s  %10s  %6s  %s\n", "MODEL", "SIZE", "DOWNLOADS", "LIKES", "ARCH")
		fmt.Printf("%-50s  %7s  %10s  %6s  %s\n", "-----", "----", "---------", "-----", "----")
		for _, m := range models {
			compat, reason := m.CheckCompat()
			indicator := "✓"
			if !compat {
				indicator = "✗"
			}
			fmt.Printf("%-50s  %7s  %10d  %6d  %s %s\n", m.ID, m.SizeLabel(), m.Downloads, m.Likes, indicator, reason)
		}
		return nil
	},
}

var modelsInfoCmd = &cobra.Command{
	Use:   "info [model-id]",
	Short: "Show details about a HuggingFace model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		info, err := hf.Info(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("ID:        %s\n", info.ID)
		fmt.Printf("Pipeline:  %s\n", info.Pipeline)
		fmt.Printf("Downloads: %d\n", info.Downloads)
		fmt.Printf("Likes:     %d\n", info.Likes)
		fmt.Printf("Tags:      %v\n", info.Tags)
		if len(info.Siblings) > 0 {
			fmt.Println("\nFiles:")
			for _, f := range info.Siblings {
				if f.Size > 0 {
					fmt.Printf("  %-60s  %.1f MB\n", f.Filename, float64(f.Size)/1024/1024)
				} else {
					fmt.Printf("  %s\n", f.Filename)
				}
			}
		}
		return nil
	},
}

func init() {
	modelsSearchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 20, "Number of results")
	modelsCmd.AddCommand(modelsSearchCmd)
	modelsCmd.AddCommand(modelsInfoCmd)
}
