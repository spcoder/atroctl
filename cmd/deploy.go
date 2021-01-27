package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	dir      string
	strategy string
)

// deployCmd represents the deploy command
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "deploy to atrocity",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("deploy called")
		fmt.Printf("url: %s\n", url)
		fmt.Printf("strategy: %s\n", strategy)
		files, err := glob(dir, ".js")
		if err != nil {
			fmt.Printf("crap: %s", err)
			return
		}
		for _, file := range files {
			fmt.Println(file)
		}
	},
}

func glob(dir string, ext string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if filepath.Ext(path) == ext {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func init() {
	deployCmd.Flags().StringVarP(&dir, "dir", "d", ".", "the directory to scan for js files")
	deployCmd.Flags().StringVarP(&strategy, "strategy", "s", "bluegreen", "the deployment strategy (bluegreen, githead)")
	rootCmd.AddCommand(deployCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// deployCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// deployCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
