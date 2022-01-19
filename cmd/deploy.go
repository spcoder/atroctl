package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"

	"github.com/spf13/cobra"

	"github.com/evanw/esbuild/pkg/api"
)

var (
	funcDir   string
	staticDir string
	strategy  string
	watch     bool
)

const (
	atroctlFuncDir   = "ATROCTL_FUNC_DIR"
	atroctlStaticDir = "ATROCTL_STATIC_DIR"
	atroctlStrategy  = "ATROCTL_STRATEGY"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "deploy to Atrocity",
	Long: `Deploy to Atrocity.

Strategies:
When deploying you'll need to choose a strategy.
* bluegreen = rotates between blue and green deployments
* gitrev    = (TBD) uses 'git rev-parse HEAD' as the deployment id
* uuid      = (TBD) uses a random uuid as the deployment id

Secrets:
Any environment variable that starts with ATROCITY_ will be deployed to Atrocity.
The secret will be available to atrocity functions without the ATROCITY_. For example,
ATROCITY_PG_CONNECTION will be available as PG_CONNECTION.

Examples:
  # deploys all *.js files recursively in the "src" directory to http://localhost:9090 using the bluegreen strategy
  atroctl deploy

  # deploys all *.js files recursively in ~/dev/project/server to https://example.com using the latest revision number in the current directory
  # also, deploys all files (except hidden) in ~/dev/project/assets to https://example.com as static assets using the same strategy
  atroctl deploy -f ~/dev/project/server -s ~/dev/project/assets -u https://example.com -s gitrev`,
	Args: cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		printHeader(cmd.Parent().Version)

		strategyFunc, err := resolveStrategy()
		if err != nil {
			return err
		}

		err = strategyFunc()
		if err != nil {
			p("error", "%s\n", err)
			return err
		}

		if watch {
			err = startWatching(strategyFunc)
			if err != nil {
				return err
			}
		}

		return nil
	},
}

type strategyFunction func() error

func resolveStrategy() (strategyFunction, error) {
	switch strategy {
	case "bluegreen":
		return bluegreen, nil
	default:
		return nil, fmt.Errorf("strategy (%s) not supported", strategy)
	}
}

func printHeader(version string) {
	p("atroctl", "version %s\n", version)
	p("atroctl", "starting deployment to %s\n", url)
	p("strategy", "using strategy %s\n", strategy)
}

func p(key, msg string, args ...interface{}) {
	if key == "" {
		fmt.Printf(msg, args...)
		return
	}
	fmt.Printf("%10s: %s", strings.ToUpper(key), fmt.Sprintf(msg, args...))
}

func startWatching(fn strategyFunction) error {
	p("watch", "starting to watch directories for changes\n")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					fmt.Printf("\n\n")
					p("watch", "detected file system change\n")
					err = fn()
					if err != nil {
						p("error", "%s\n", err)
					}
					fmt.Print("\n\n\a")
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				p("error", "%s\n", err)
			}
		}
	}()

	err = filepath.WalkDir(funcDir, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			p("watch", path+"\n")
			return watcher.Add(path)
		}
		return nil
	})
	if staticDir != "" {
		err = filepath.WalkDir(staticDir, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				p("watch", path+"\n")
				return watcher.Add(path)
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	<-done

	return nil
}

func httpCall(method, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if apiKey != "" {
		req.Header.Set("API_KEY", apiKey)
	}
	if apiSecretKey != "" {
		req.Header.Set("API_SECRET_KEY", apiSecretKey)
	}
	client := &http.Client{}
	return client.Do(req)
}

func httpPut(url, contentType string, body io.Reader) (*http.Response, error) {
	return httpCall(http.MethodPut, url, contentType, body)
}

func httpPost(url, contentType string, body io.Reader) (*http.Response, error) {
	return httpCall(http.MethodPost, url, contentType, body)
}

func httpGet(url string) (*http.Response, error) {
	return httpCall(http.MethodGet, url, "text/plain", nil)
}

func bluegreen() error {
	deployId, err := getDeployId()
	if err != nil {
		return fmt.Errorf("error getting deploy id: %w", err)
	}
	if deployId == "blue" {
		deployId = "green"
	} else if deployId == "green" {
		deployId = "blue"
	} else {
		return fmt.Errorf("failed to get current deploy id")
	}
	return deploy(deployId)
}

func deploy(deployId string) error {
	p(strategy, "deploying to %s\n", deployId)
	err := beginDeployment(deployId)
	if err != nil {
		return err
	}
	err = deploySecrets(deployId)
	if err != nil {
		return err
	}
	err = deployFunction(deployId)
	if err != nil {
		return err
	}
	err = deployStatics(deployId)
	if err != nil {
		return err
	}
	err = activateDeployment(deployId)
	if err != nil {
		return err
	}
	return nil
}

func getDeployId() (string, error) {
	resp, err := httpGet(fmt.Sprintf("%s/deploy", url))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func deploySecrets(deployId string) error {
	p("secrets", "starting to deploy secrets\n")
	for _, v := range envvars() {
		p("secrets", "deploying %s", v.key)
		resp, err := httpPut(fmt.Sprintf("%s/deploy/%s/secret/%s", url, deployId, v.key), "text/plain", strings.NewReader(v.value))
		if err != nil {
			return fmt.Errorf("error deploying secret (%s): %w", v.key, err)
		}
		if resp.StatusCode == http.StatusNoContent {
			p("", " [OK]\n")
		} else {
			p("", " [%d]\n", resp.StatusCode)
			return fmt.Errorf("failed to deploy secret (%s)", v.key)
		}
	}
	p("secrets", "successfully deployed\n")
	return nil
}

func bundle() ([]byte, error) {
	entryFile := path.Join(funcDir, "index.js")
	result := api.Build(api.BuildOptions{
		Bundle:      true,
		EntryPoints: []string{entryFile},
		Platform:    api.PlatformNode,
		LogLevel:    api.LogLevelInfo,
	})
	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			fmt.Println(err.Text)
		}
		return nil, errors.New("error while bundling")
	}
	return result.OutputFiles[0].Contents, nil
}

func deployFunction(deployId string) error {
	p("functions", "starting to deploy functions in '%s'\n", funcDir)
	p("functions", "creating bundle")
	content, err := bundle()
	if err != nil {
		return err
	}
	p("", " [OK]\n")

	p("functions", "deploying bundle")
	resp, err := httpPut(fmt.Sprintf("%s/deploy/%s/function", url, deployId), "text/plain", bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("error deploying bundle: %w", err)
	}
	if resp.StatusCode == http.StatusNoContent {
		p("", " [OK]\n")
	} else {
		p("", " [%d]\n", resp.StatusCode)
		return fmt.Errorf("failed to deploy bundle")
	}
	p("functions", "successfully deployed\n")
	return nil
}

func deployStatics(deployId string) error {
	if staticDir == "" {
		return nil
	}
	p("statics", "starting to deploy static files in '%s'\n", staticDir)
	files, err := globAll(staticDir)
	if err != nil {
		return fmt.Errorf("error globbing files: %w", err)
	}
	for _, f := range files {
		p("statics", "deploying file %s", f)
		contents, err := ioutil.ReadFile(f)
		if err != nil {
			return fmt.Errorf("error reading file (%s): %w", f, err)
		}
		fpath := filepath.ToSlash(removeDir(f, staticDir))
		contentType := http.DetectContentType(contents)
		resp, err := httpPut(fmt.Sprintf("%s/deploy/%s/static/%s", url, deployId, fpath), contentType, bytes.NewReader(contents))
		if err != nil {
			return fmt.Errorf("error deploying static file (%s): %w", f, err)
		}
		if resp.StatusCode == http.StatusNoContent {
			p("", " [OK]\n")
		} else {
			p("", " [%d]\n", resp.StatusCode)
			return fmt.Errorf("failed to deploy static file (%s)", f)
		}
	}
	p("statics", "successfully deployed\n")
	return nil
}

func removeDir(f, dir string) string {
	s := strings.Replace(f, dir, "", 1)
	if strings.HasPrefix(s, "/") {
		return s[1:]
	}
	return s
}

func beginDeployment(deployId string) error {
	p("begin", "starting deployment %s\n", deployId)
	resp, err := httpPost(fmt.Sprintf("%s/deploy/%s/begin", url, deployId), "text/plain", nil)
	if err != nil {
		return fmt.Errorf("error starting deployment: %w", err)
	}
	if resp.StatusCode == http.StatusNoContent {
		p("begin", "successfully started deployment %s\n", deployId)
	} else {
		return fmt.Errorf("failed to begin deployment: %w", err)
	}
	return nil
}

func activateDeployment(deployId string) error {
	p("activate", "starting to activate %s\n", deployId)
	resp, err := httpPost(fmt.Sprintf("%s/deploy/%s/activate", url, deployId), "text/plain", nil)
	if err != nil {
		return fmt.Errorf("error activating deployment: %w", err)
	}
	if resp.StatusCode == http.StatusNoContent {
		p("activate", "successfully activated %s\n", deployId)
	} else {
		return fmt.Errorf("failed to activate deployment: %w", err)
	}
	return nil
}

func globAll(dir string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

type keyval struct {
	key   string
	value string
}

func envvars() []keyval {
	result := make([]keyval, 0)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		if strings.HasPrefix(pair[0], "ATROCITY_") {
			key := strings.Replace(pair[0], "ATROCITY_", "", 1)
			result = append(result, keyval{key, pair[1]})
		}
	}
	return result
}

func resolveStringFlag(value, envvar, fallback string) string {
	if value == "" {
		value = os.Getenv(envvar)
	}
	if value == "" {
		return fallback
	}
	return value
}

func init() {
	deployCmd.Flags().StringVarP(&funcDir, "funcDir", "f", "", fmt.Sprintf("the directory that contains functions to deploy [%s]", atroctlFuncDir))
	deployCmd.Flags().StringVarP(&staticDir, "staticDir", "s", "", fmt.Sprintf("the directory that contains static assets to deploy [%s]", atroctlStaticDir))
	deployCmd.Flags().StringVarP(&strategy, "strategy", "g", "", fmt.Sprintf("the deployment strategy (bluegreen, gitrev, uuid) [%s]", atroctlStrategy))
	deployCmd.Flags().BoolVarP(&watch, "watch", "w", false, "deploy when directory changes")
	rootCmd.AddCommand(deployCmd)

	funcDir = resolveStringFlag(funcDir, atroctlFuncDir, "src")
	staticDir = resolveStringFlag(staticDir, atroctlStaticDir, "")
	strategy = resolveStringFlag(strategy, atroctlStaticDir, "bluegreen")
}
