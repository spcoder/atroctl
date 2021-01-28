package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var (
	url       string
	apikey    string
	secretkey string
)

var rootCmd = &cobra.Command{
	Use:     "atroctl",
	Version: "1.0.0",
	Short:   "atroctl controls Atrocity instances",
	Long:    `The atroctl command lets you control instances of Atrocity.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&url, "url", "u", "http://localhost:9090", "the url to atrocity")
	rootCmd.PersistentFlags().StringVarP(&apikey, "apikey", "k", "", "the api key")
	rootCmd.PersistentFlags().StringVarP(&secretkey, "secretkey", "x", "", "the api secret key")
}
