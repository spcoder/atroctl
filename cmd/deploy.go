package cmd

import (
	"bytes"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	dir      string
	strategy string
	watch    bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy [directory]",
	Short: "deploy to Atrocity",
	Long: `Deploy to Atrocity.

Strategies:
When deploying you'll need to choose a strategy.
* bluegreen = rotates between blue and green deployments
* gitrev    = uses 'git rev-parse HEAD' as the deployment id
* uuid      = uses a random uuid as the deployment id

Secrets:
Any environment variable that starts with ATROCITY_ will be deployed to Atrocity.
The secret will be available to atrocity functions without the ATROCITY_. For example,
ATROCITY_PG_CONNECTION will be available as PG_CONNECTION.

Examples:
  # deploys all *.js files in the current directory to http://localhost:9090 using the bluegreen strategy
  atroctl deploy

  # deploys all *.js files recursively in ~/dev/project/server to http://example.com using the latest revision number in the current directory
  atroctl deploy ~/dev/project/server -u http://example.com -s gitrev`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		printHeader(cmd.Parent().Version)
		resolveDir(args)

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

func resolveDir(args []string) {
	if len(args) == 0 {
		dir = "."
	} else {
		dir = args[0]
	}
}

func p(key, msg string, args ...interface{}) {
	if key == "" {
		fmt.Printf(msg, args...)
		return
	}
	fmt.Printf("%10s: %s", strings.ToUpper(key), fmt.Sprintf(msg, args...))
}

func startWatching(fn strategyFunction) error {
	p("watch", "starting to watch directory for changes\n")
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
					p("watch", "detected file system change\n")
					err = fn()
					if err != nil {
						p("error", "%s\n", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				p("error", "%s\n", err)
			}
		}
	}()

	err = watcher.Add(dir)
	if err != nil {
		return err
	}
	<-done

	return nil
}

func httpPut(url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	client := &http.Client{}
	return client.Do(req)
}

func bluegreen() error {
	deployId, err := getDeployId()
	if err != nil {
		return fmt.Errorf("error getting deploy id: %w", err)
	}
	if deployId == "blue" {
		deployId = "green"
	} else {
		deployId = "blue"
	}
	return deploy(deployId)
}

func deploy(deployId string) error {
	p(strategy, "deploying to %s\n", deployId)
	err := deploySecrets(deployId)
	if err != nil {
		return err
	}
	err = deployFunctions(deployId)
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
	resp, err := http.Get(fmt.Sprintf("%s/deploy", url))
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

func deployFunctions(deployId string) error {
	p("functions", "starting to deploy functions in '%s'\n", dir)
	files, err := glob(dir, ".js")
	if err != nil {
		return fmt.Errorf("error globbing files: %w", err)
	}
	for _, f := range files {
		p("functions", "deploying file %s", f)
		contents, err := ioutil.ReadFile(f)
		if err != nil {
			return fmt.Errorf("error reading file (%s): %w", f, err)
		}
		basename := filepath.Base(f)
		resp, err := httpPut(fmt.Sprintf("%s/deploy/%s/function/%s", url, deployId, basename), "text/plain", bytes.NewReader(contents))
		if err != nil {
			return fmt.Errorf("error deploying function (%s): %w", f, err)
		}
		if resp.StatusCode == http.StatusNoContent {
			p("", " [OK]\n")
		} else {
			p("", " [%d]\n", resp.StatusCode)
			return fmt.Errorf("failed to deploy function (%s)", f)
		}
	}
	p("functions", "successfully deployed\n")
	return nil
}

func activateDeployment(deployId string) error {
	p("activate", "starting to activate %s\n", deployId)
	resp, err := http.Post(fmt.Sprintf("%s/deploy/%s/activate", url, deployId), "text/plain", nil)
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

func init() {
	deployCmd.Flags().StringVarP(&strategy, "strategy", "s", "bluegreen", "the deployment strategy (bluegreen, gitrev, uuid)")
	deployCmd.Flags().BoolVarP(&watch, "watch", "w", false, "deploy when directory changes")
	rootCmd.AddCommand(deployCmd)
}
