package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/buildtools/build"

	"github.com/bazelbuild/rules_go/go/runfiles"

	"github.com/fmeum/auto_use_repo/update"
)

func main() {
	moduleFilePath := filepath.Join(os.Getenv("BUILD_WORKSPACE_DIRECTORY"), "MODULE.bazel")
	moduleFileContent, err := os.ReadFile(moduleFilePath)
	if err != nil {
		log.Fatalf("Failed to read %s: %v", moduleFilePath, err)
	}
	moduleFile, err := build.ParseModule(moduleFilePath, moduleFileContent)
	if err != nil {
		log.Fatalf("Failed to parse %s: %v", moduleFilePath, err)
	}

	repoLists := make(map[string][]string)
	for _, arg := range os.Args[1:] {
		split := strings.SplitN(arg, "=", 2)
		if len(split) != 2 {
			log.Fatalf("Invalid argument: %s", arg)
		}
		extension, repoFileRlocation := split[0], split[1]
		repoListPath, err := runfiles.Rlocation(repoFileRlocation)
		if err != nil {
			log.Fatalf("Could not find runfile %s: %v", repoFileRlocation, err)
		}
		repoListContent, err := os.ReadFile(repoListPath)
		if err != nil {
			log.Fatalf("Failed to read %s: %v", repoListPath, err)
		}
		var repoList []string
		err = json.Unmarshal(repoListContent, &repoList)
		if err != nil {
			log.Fatalf("Failed to parse %s: %v", repoListPath, err)
		}

		repoLists[extension] = repoList
	}

	err = update.UpdateRepoUsages(moduleFile, repoLists)
	if err != nil {
		log.Fatalf("Failed to update repo usages: %v", err)
	}
	moduleFileContent = build.Format(moduleFile)
	err = os.WriteFile(moduleFilePath, moduleFileContent, 0644)
	if err != nil {
		log.Fatalf("Failed to write %s: %v", moduleFilePath, err)
	}
}
