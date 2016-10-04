package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/convox/rack/cmd/convox/stdcli"
	"github.com/convox/rack/manifest"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/equinox-io/equinox"
	cli "gopkg.in/urfave/cli.v1"
)

func init() {
	stdcli.RegisterCommand(cli.Command{
		Name:        "doctor",
		Action:      cmdDoctor,
		Description: "Check your app for common Convox compatibility issues.",
	})
}

type Diagnosis struct {
	DocsLink    string
	Kind        string
	Description string
}

var (
	diagnoses = []Diagnosis{}

	buildChecks = []func(*manifest.Manifest) error{
		checkDockerIgnore,
	}
	// devChecks   = []func(*manifest.Manifest) error{}
	// prodChecks  = []func(*manifest.Manifest) error{}

	// manifestChecks = []func(*manifest.Manifest) error{
	// 	// checkCLIVersion,
	// 	validateManifest,
	// 	checkDockerIgnore,
	// 	checkMissingDockerFiles,
	// 	syncVolumeConflict,
	// 	missingEnvValues,
	// 	checkLargeFiles,
	// }

	// dockerChecks = []func() error{
	// 	dockerTest,
	// }
)

func diagnose(d Diagnosis) {
	log.Printf("diagnosing %#v", d)
	diagnoses = append(diagnoses, d)
}

func medicalReport() {
	if len(diagnoses) > 0 {
		fmt.Printf("%#v", diagnoses)
		os.Exit(1)
	}
}

func cmdDoctor(c *cli.Context) error {
	m, err := manifest.LoadFile("docker-compose.yml")
	if err != nil {
		return stdcli.ExitError(err)
	}

	for _, check := range buildChecks {
		if err := check(m); err != nil {
			return stdcli.ExitError(err)
		}
	}

	medicalReport()
	// for _, check := range dockerChecks {
	// 	if err := check(); err != nil {
	// 		return stdcli.ExitError(err)
	// 	}
	// }

	fmt.Println("Everything looks fine, deploy and pay us all your moneyz")
	return nil
}

func checkDockerIgnore(m *manifest.Manifest) error {
	log.Printf("HERE1")
	_, err := os.Stat(".dockerignore")
	if err != nil {
		diagnose(Diagnosis{
			Kind:        "security",
			DocsLink:    "#TODO",
			Description: "You should probably have a .dockerignore file",
		})
		return nil
	}

	log.Printf("HERE2")
	// read the whole file at once
	b, err := ioutil.ReadFile(".dockerignore")
	if err != nil {
		return err
	}
	s := string(b)

	log.Printf("HERE3")
	// //check whether s contains substring text
	if !strings.Contains(s, ".git\n") {
		diagnose(Diagnosis{
			Kind:        "security",
			DocsLink:    "#TODO",
			Description: "You should probably add .git to your .dockerignore",
		})
	}

	log.Printf("HERE4")
	log.Print(s)
	if !strings.Contains(s, ".env\n") {
		log.Printf("HERE5")
		diagnose(Diagnosis{
			Kind:        "security",
			DocsLink:    "#TODO",
			Description: "You should probably add .env to your .dockerignore",
		})
	}

	return nil
}

func checkCLIVersion(m *manifest.Manifest) error {
	client, err := updateClient()
	if err != nil {
		return stdcli.ExitError(err)
	}

	opts := equinox.Options{
		CurrentVersion: Version,
		Channel:        "stable",
		HTTPClient:     client,
	}
	if err := opts.SetPublicKeyPEM(publicKey); err != nil {
		return stdcli.ExitError(err)
	}

	// check for update
	_, err = equinox.Check("app_i8m2L26DxKL", opts)
	if err == nil {
		return errors.New("Your client is out of date, run `convox update`")
	}
	return nil
}

func validateManifest(m *manifest.Manifest) error {
	return m.Validate()
}

func missingEnvValues(m *manifest.Manifest) error {
	if denv := filepath.Join(filepath.Dir(os.Args[0]), ".env"); exists(denv) {
		data, err := ioutil.ReadFile(denv)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))

		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "=") {
				parts := strings.SplitN(scanner.Text(), "=", 2)

				err := os.Setenv(parts[0], parts[1])
				if err != nil {
					return err
				}
			}
		}

		if err := scanner.Err(); err != nil {
			return err
		}
	}

	// check for required env vars
	existing := map[string]bool{}
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			existing[parts[0]] = true
		}
	}

	for _, s := range m.Services {
		links := map[string]bool{}

		for _, l := range s.Links {
			key := fmt.Sprintf("%s_URL", strings.ToUpper(l))
			links[key] = true
		}

		missingEnv := []string{}
		for key, val := range s.Environment {
			eok := val != ""
			_, exok := existing[key]
			_, lok := links[key]
			if !eok && !exok && !lok {
				missingEnv = append(missingEnv, key)
			}
		}

		if len(missingEnv) > 0 {
			return fmt.Errorf("env expected: %s", strings.Join(missingEnv, ", "))
		}
	}

	return nil
}

func syncVolumeConflict(m *manifest.Manifest) error {
	for _, s := range m.Services {
		sps, err := s.SyncPaths()
		if err != nil {
			return err
		}

		for _, v := range s.Volumes {
			parts := strings.Split(v, ":")
			if len(parts) == 2 {
				for k, _ := range sps {
					if k == parts[0] {
						return fmt.Errorf("service: %s has a sync path conflict with volume %s", s.Name, v)
					}
				}
			}
		}
	}
	return nil
}

func checkMissingDockerFiles(m *manifest.Manifest) error {
	for _, s := range m.Services {
		if s.Image == "" {
			dockerFile := coalesce(s.Dockerfile, "Dockerfile")
			dockerFile = coalesce(s.Build.Dockerfile, dockerFile)
			_, err := os.Stat(fmt.Sprintf("%s/%s", s.Build.Context, dockerFile))
			if err != nil {
				return fmt.Errorf("service: %s is missing it's Dockerfile", s.Name)
			}
		}
	}
	return nil
}

func checkLargeFiles(m *manifest.Manifest) error {
	di, err := readDockerIgnore(".")
	if err != nil {
		return err
	}

	f := func(path string, info os.FileInfo, err error) error {
		m, err := fileutils.Matches(path, di)
		if err != nil {
			return err
		}
		if m {
			return nil
		}
		if info.Size() >= 200000000 {
			return fmt.Errorf("%s is %d, perhaps you should add it to your .dockerignore to speed up builds and deploys", info.Name(), info.Size())
		}
		return nil
	}

	err = filepath.Walk(".", f)
	if err != nil {
		return err
	}
	return nil
}
