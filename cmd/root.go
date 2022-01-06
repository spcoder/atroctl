package cmd

import (
	"fmt"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/cobra"
)

var (
	url          string
	apiKey       string
	apiSecretKey string
)

const (
	atroctlUrl          = "ATROCTL_URL"
	atroctlApiKey       = "ATROCTL_API_KEY"
	atroctlApiSecretKey = "ATROCTL_API_SECRET_KEY"
)

var rootCmd = &cobra.Command{
	Use:     "atroctl",
	Version: "1.3.0",
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
	rootCmd.PersistentFlags().StringVarP(&url, "url", "u", "", fmt.Sprintf("the url to atrocity [%s]", atroctlUrl))
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "", "", fmt.Sprintf("the api key [%s]", atroctlApiKey))
	rootCmd.PersistentFlags().StringVarP(&apiSecretKey, "api-secret-key", "", "", fmt.Sprintf("the api secret key [%s]", atroctlApiSecretKey))

	if url == "" {
		url = os.Getenv(atroctlUrl)
		if url == "" {
			url = "http://localhost:9090"
		}
	}

	if apiKey == "" {
		apiKey = os.Getenv(atroctlApiKey)
	}

	if apiSecretKey == "" {
		apiSecretKey = os.Getenv(atroctlApiSecretKey)
	}
}
