package terraform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar"
	"github.com/env0/terratag/internal/convert"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/thoas/go-funk"
)

func GetTerraformVersion() (*convert.Version, error) {
	output, err := exec.Command("terraform", "version").Output()
	if err != nil {
		return nil, err
	}

	outputAsString := strings.TrimSpace(string(output))
	regularExpression := regexp.MustCompile(`Terraform v(\d+).(\d+)\.\d+`)
	matches := regularExpression.FindStringSubmatch(outputAsString)[1:]

	if matches == nil {
		return nil, errors.New("unable to parse 'terraform version'")
	}

	majorVersion, err := getVersionPart(matches, Major)
	if err != nil {
		return nil, err
	}
	minorVersion, err := getVersionPart(matches, Minor)
	if err != nil {
		return nil, err
	}

	if (majorVersion == 0 && minorVersion < 11 || minorVersion > 15) || (majorVersion == 1 && minorVersion > 1) {
		return nil, fmt.Errorf("terratag only supports Terraform from version 0.11.x and up to 1.1.x - your version says %s", outputAsString)
	}

	return &convert.Version{Major: majorVersion, Minor: minorVersion}, nil
}

type VersionPart int

const (
	Major VersionPart = iota
	Minor
)

func (w VersionPart) EnumIndex() int {
	return int(w)
}

func getVersionPart(parts []string, versionPart VersionPart) (int, error) {
	version, err := strconv.Atoi(parts[versionPart])
	if err != nil {
		return -1, fmt.Errorf("unable to parse %s as integer", parts[versionPart])
	}

	return version, nil
}

func GetResourceType(resource hclwrite.Block) string {
	return resource.Labels()[0]
}

func ValidateTerraformInitRun(dir string) error {
	_, err := os.Stat(dir + "/.terraform")

	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("terraform init must run before running terratag")
		}

		return fmt.Errorf("couldn't determine if terraform init has run: %v", err)
	}

	return nil
}

func GetTerraformFilePaths(rootDir string) ([]string, error) {
	const tfFileMatcher = "/*.tf"

	tfFiles, err := doublestar.Glob(rootDir + tfFileMatcher)
	if err != nil {
		return nil, err
	}

	modulesDirs, err := getTerraformModulesDirPaths(rootDir)
	if err != nil {
		return nil, err
	}

	for _, moduleDir := range modulesDirs {
		matches, err := doublestar.Glob(moduleDir + tfFileMatcher)
		if err != nil {
			return nil, err
		}

		tfFiles = append(tfFiles, matches...)
	}

	for i, tfFile := range tfFiles {
		resolvedTfFile, err := filepath.EvalSymlinks(tfFile)
		if err != nil {
			return nil, err
		}

		tfFiles[i] = resolvedTfFile
	}

	return funk.UniqString(tfFiles), nil
}

func getTerraformModulesDirPaths(dir string) ([]string, error) {
	var paths []string
	var modulesJson ModulesJson

	jsonFile, err := os.Open(dir + "/.terraform/modules/modules.json")
	//lint:ignore SA5001 not required to check file close status.
	defer jsonFile.Close()

	if os.IsNotExist(err) {
		return paths, nil
	}

	byteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(byteValue, &modulesJson); err != nil {
		return nil, err
	}

	for _, module := range modulesJson.Modules {
		modulePath, err := filepath.EvalSymlinks(dir + "/" + module.Dir)
		if os.IsNotExist(err) {
			log.Print("[WARN] Module not found, skipping.", dir+"/"+module.Dir)
			continue
		}

		if err != nil {
			return nil, err
		}

		paths = append(paths, modulePath)
	}

	return paths, nil
}

type ModulesJson struct {
	Modules []ModuleMetadata `json:"Modules"`
}

type ModuleMetadata struct {
	Key    string `json:"Key"`
	Source string `json:"Source"`
	Dir    string `json:"Dir"`
}
