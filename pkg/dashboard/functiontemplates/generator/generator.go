// +build ignore

/*
Copyright 2017 The Nuclio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This program generates function template sources in pkg/dashboard/functiontemplates/generated.go
// It can be invoked by running go generate

package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/nuclio/nuclio/pkg/common"
	"github.com/nuclio/nuclio/pkg/dashboard/functiontemplates"
	"github.com/nuclio/nuclio/pkg/errors"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/processor/build/inlineparser"

	"github.com/ghodss/yaml"
	"github.com/nuclio/logger"
	"github.com/nuclio/zap"
	yamlv2 "gopkg.in/yaml.v2"
)

var funcMap = template.FuncMap{

	// used inside the template to pack configuration objects into text (so that they're printed nicely)
	"marshalConfig": func(data interface{}) string {
		bytes, _ := yaml.Marshal(data)
		return string(bytes)
	},

	// function source code (and marshalled configurations) may contain backticks. since they're written inside raw,
	// backtick-quoted strings in the generated code, those must be escaped away
	"escapeBackticks": func(s string) string {
		return strings.Replace(s, "`", "`"+" + \"`\" + "+"`", -1)
	},

	"join": strings.Join,
}

var packageTemplate = template.Must(template.New("").Funcs(funcMap).Parse(`// Code generated by go generate; DO NOT EDIT.

/*
This machine-generated file contains the configuration and source code for function templates,
which may be retrieved from the dashboard's HTTP API by sending a GET request to /function_templates.

The following functions are included for each supported runtime:
{{- range $runtime, $functions := .FunctionsByRuntime }}
{{ printf "%s (%d):" $runtime (len $functions) | printf "%-15s" }} {{ join $functions ", " }}
{{- end }}
*/

package functiontemplates

import (
	"github.com/nuclio/nuclio/pkg/functionconfig"

	"github.com/ghodss/yaml"
)

var FunctionTemplates = []*FunctionTemplate{
{{- range .FunctionTemplates }}
	{
		Name: {{ printf "%q" .Name }},
		Configuration: unmarshalConfig(` + "`" + `{{ marshalConfig .Configuration | escapeBackticks }}` + "`" + `),
		SourceCode: ` + "`" + `{{ escapeBackticks .SourceCode }}` + "`" + `,
	},
{{- end }}
}

// no error checking is performed here. this is guaranteed to work, because the strings fed to this function
// are marshalled representations of actual configuration objects that were created while generating this file
func unmarshalConfig(marshalledConfig string) functionconfig.Config {
	config := functionconfig.Config{}

	err := yaml.Unmarshal([]byte(marshalledConfig), &config)
	if err != nil {
		panic("failed to unmarshal marshaled config")
	}

	return config
}
`))

type Generator struct {
	logger        logger.Logger
	examplesDir   string
	outputPath    string
	runtimes      []string
	inlineParsers map[string]*inlineparser.InlineParser
	functions     map[string][]string
}

func Run(examplesDir string, outputPath string) error {
	logger, err := createLogger()
	if err != nil {
		return errors.Wrap(err, "Failed to create logger")
	}

	generator, err := createGenerator(logger, examplesDir, outputPath)
	if err != nil {
		return errors.Wrap(err, "Failed to create generator")
	}

	err = generator.generate()
	if err != nil {
		return errors.Wrap(err, "Failed to generate function template sources")
	}

	return nil
}

func (g *Generator) generate() error {
	if err := g.verifyPaths(); err != nil {
		return errors.Wrap(err, "Failed to verify paths")
	}

	functionDirs, err := g.detectFunctionDirs()
	if err != nil {
		return errors.Wrap(err, "Failed to detect functions in given examples directory")
	}

	functionTemplates, err := g.buildFunctionTemplates(functionDirs)
	if err != nil {
		return errors.Wrap(err, "Failed to build function templates")
	}

	if err = g.writeOutputFile(functionTemplates); err != nil {
		return errors.Wrap(err, "Failed to write output file")
	}

	g.logger.Info("Done")

	return nil
}

func (g *Generator) verifyPaths() error {
	if !common.IsDir(g.examplesDir) {
		return errors.Errorf("Given examples directory is not a directory: %s", g.examplesDir)
	}

	g.logger.DebugWith("Verified examples directory exists", "path", g.examplesDir)

	return nil
}

func (g *Generator) detectFunctionDirs() ([]string, error) {
	var functionDirs []string

	g.logger.DebugWith("Looking for function directories inside runtime directories", "runtimes", g.runtimes)

	for _, runtime := range g.runtimes {
		g.functions[runtime] = []string{}
		runtimeDir := filepath.Join(g.examplesDir, runtime)

		// traverse each runtime directory, look for function dirs inside it
		err := filepath.Walk(runtimeDir, func(path string, info os.FileInfo, err error) error {

			// handle any failure to walk over a specific file
			if err != nil {
				g.logger.WarnWith("Failed to walk over file at path", "path", path)
				return errors.Wrapf(err, "Failed to walk over file at path %s", path)
			}

			// if the file is a directory and resides directly under the runtime directory, it's a function directory
			if info.IsDir() && filepath.Base(filepath.Dir(path)) == runtime {
				g.logger.DebugWith("Found function directory", "runtime", runtime, "name", filepath.Base(path))

				// append the directory to our slice
				functionDirs = append(functionDirs, path)

				return nil
			}

			// otherwise do nothing
			return nil
		})

		if err != nil {
			return nil, errors.Wrapf(err, "Failed to walk %s runtime directory", runtime)
		}
	}

	return functionDirs, nil
}

func (g *Generator) buildFunctionTemplates(functionDirs []string) ([]*functiontemplates.FunctionTemplate, error) {
	var functionTemplates []*functiontemplates.FunctionTemplate

	g.logger.DebugWith("Building function templates", "numFunctions", len(functionDirs))

	for _, functionDir := range functionDirs {
		runtimeName := filepath.Base(filepath.Dir(functionDir))

		configuration, sourceCode, err := g.getFunctionConfigAndSource(functionDir, runtimeName)
		if err != nil {
			g.logger.WarnWith("Failed to get function configuration and source code",
				"err", err,
				"functionDir", functionDir)

			return nil, errors.Wrap(err, "Failed to get function configuration and source code")
		}

		functionName := filepath.Base(functionDir)

		functionTemplate := functiontemplates.FunctionTemplate{
			Configuration: *configuration,
			Name:          functionName,
			SourceCode:    sourceCode,
		}

		g.logger.InfoWith("Appending function template", "functionName", functionName, "runtime", runtimeName)
		functionTemplates = append(functionTemplates, &functionTemplate)
		g.functions[runtimeName] = append(g.functions[runtimeName], functionName)
	}

	return functionTemplates, nil
}

func (g *Generator) getFunctionConfigAndSource(functionDir string,
	runtime string) (*functionconfig.Config, string, error) {

	configuration := functionconfig.Config{}
	sourceCode := ""

	// we'll know later not to look for an inline config if this is set
	configFileExists := false

	// first, look for a function.yaml file. parse it if found
	configPath := filepath.Join(functionDir, "function.yaml")

	if common.IsFile(configPath) {
		configFileExists = true

		configContents, err := ioutil.ReadFile(configPath)
		if err != nil {
			return nil, "", errors.Wrapf(err, "Failed to read function configuration file at %s", configPath)
		}

		if err = yaml.Unmarshal(configContents, &configuration); err != nil {
			return nil, "", errors.Wrapf(err, "Failed to unmarshal function configuration file at %s", configPath)
		}
	}

	// look for the first non-function.yaml file - this is our source code
	// (multiple-source function templates not yet supported)
	files, err := ioutil.ReadDir(functionDir)
	if err != nil {
		return nil, "", errors.Wrapf(err, "Failed to list function directory at %s", functionDir)
	}

	for _, file := range files {
		if file.Name() != "function.yaml" {

			// we found our source code, read it
			sourcePath := filepath.Join(functionDir, file.Name())

			sourceBytes, err := ioutil.ReadFile(sourcePath)
			if err != nil {
				return nil, "", errors.Wrapf(err, "Failed to read function source code at %s", sourcePath)
			}

			if len(sourceBytes) == 0 {
				return nil, "", errors.Errorf("Function source code at %s is empty", sourcePath)
			}

			sourceCode = string(sourceBytes)

			// if there was no function.yaml, parse the inline config from the source code
			// TODO: delete it from source too
			if !configFileExists {
				err = g.parseInlineConfiguration(sourcePath, &configuration, runtime)
				if err != nil {
					return nil, "", errors.Wrapf(err,
						"Failed to parse inline configuration from source at %s",
						sourcePath)
				}
			}

			// stop looking at other files
			break
		}
	}

	// make sure we found source code
	if sourceCode == "" {
		return nil, "", errors.Errorf("No source files found in function directory at %s", functionDir)
	}

	// set runtime explicitly on all function configs that don't have one, i.e. for UI to consume
	if configuration.Spec.Runtime == "" {
		configuration.Spec.Runtime = runtime
	}

	return &configuration, sourceCode, nil
}

func (g *Generator) parseInlineConfiguration(sourcePath string,
	configuration *functionconfig.Config,
	runtime string) error {

	inlineParser, found := g.inlineParsers[runtime]
	if !found {
		return errors.Errorf("No inline configuration parser found for runtime %s", runtime)
	}

	blocks, err := inlineParser.Parse(sourcePath)
	if err != nil {
		return errors.Wrapf(err, "Failed to parse inline configuration at %s", sourcePath)
	}

	configureBlock, found := blocks["configure"]
	if !found {
		g.logger.DebugWith("No configure block found in source code, returning empty config", "sourcePath", sourcePath)

		return nil
	}

	unmarshalledInlineConfigYAML, found := configureBlock["function.yaml"]
	if !found {
		return errors.Errorf("No function.yaml file found inside configure block at %s", sourcePath)
	}

	// must use yaml.v2 here since yaml.Marshal will err (not sure why)
	marshalledYAMLContents, err := yamlv2.Marshal(unmarshalledInlineConfigYAML)
	if err != nil {
		return errors.Wrapf(err, "Failed to marshal inline config from source at %s", sourcePath)
	}

	if err = yaml.Unmarshal(marshalledYAMLContents, configuration); err != nil {
		return errors.Wrapf(err, "Failed to unmarshal inline config from source at %s", sourcePath)
	}

	return nil
}

func (g *Generator) writeOutputFile(functionTemplates []*functiontemplates.FunctionTemplate) error {
	g.logger.DebugWith("Writing output file", "path", g.outputPath, "numFunctions", len(functionTemplates))

	outputFile, err := os.Create(g.outputPath)
	if err != nil {
		return errors.Wrap(err, "Failed to create output file")
	}

	defer func() {
		if err := outputFile.Close(); err != nil {
			panic("failed to close output file")
		}
	}()

	err = packageTemplate.Execute(outputFile, struct {
		FunctionTemplates  []*functiontemplates.FunctionTemplate
		FunctionsByRuntime map[string][]string
	}{
		FunctionTemplates:  functionTemplates,
		FunctionsByRuntime: g.functions,
	})

	if err != nil {
		return errors.Wrap(err, "Failed to execute template")
	}

	outputFileInfo, err := outputFile.Stat()
	if err != nil {
		return errors.Wrap(err, "Failed to stat output file")
	}

	g.logger.InfoWith("Output file written successfully", "len", outputFileInfo.Size())

	return nil
}

func createLogger() (logger.Logger, error) {
	return nucliozap.NewNuclioZapCmd("generator", nucliozap.DebugLevel)
}

func createGenerator(logger logger.Logger, examplesDir string, outputPath string) (*Generator, error) {
	newGenerator := Generator{
		logger:      logger,
		examplesDir: examplesDir,
		outputPath:  outputPath,
	}

	// TODO: support java parser too i guess
	//newGenerator.runtimes = []string{"golang", "python", "pypy", "nodejs", "java", "dotnetcore", "shell"}

	newGenerator.runtimes = []string{"golang", "python", "pypy", "nodejs", "dotnetcore", "shell"}

	slashSlashParser := inlineparser.NewParser(logger, "//")
	poundParser := inlineparser.NewParser(logger, "#")

	newGenerator.inlineParsers = map[string]*inlineparser.InlineParser{
		"golang":     slashSlashParser,
		"python":     poundParser,
		"pypy":       poundParser,
		"nodejs":     slashSlashParser,
		"dotnetcore": slashSlashParser,
		"shell":      poundParser,
	}

	newGenerator.functions = map[string][]string{}

	return &newGenerator, nil
}

func main() {
	examplesDir := flag.String("p", "", "Path to examples directory")
	outputPath := flag.String("o", "", "Path to output file")
	flag.Parse()

	if err := Run(*examplesDir, *outputPath); err != nil {
		errors.PrintErrorStack(os.Stderr, err, 5)

		os.Exit(1)
	}
}
