package venom

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ghodss/yaml"

	"github.com/ovh/cds/sdk/interpolate"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func getFilesPath(path []string) (filePaths []string, err error) {
	for _, p := range path {
		p = strings.TrimSpace(p)

		// no need to check err on os.stat.
		// if we put ./test/*.yml, it will fail and it's normal
		fileInfo, _ := os.Stat(p)
		if fileInfo != nil && fileInfo.IsDir() {
			p = p + string(os.PathSeparator) + "*.yml"
		}

		fpaths, err := filepath.Glob(p)
		if err != nil {
			log.Errorf("error reading files on path %q err:%v", path, err)
			return nil, errors.Wrapf(err, "error reading files on path %q", path)
		}

		for _, fp := range fpaths {
			switch ext := filepath.Ext(fp); ext {
			case ".yml", ".yaml":
				filePaths = append(filePaths, fp)
			}
		}
	}

	sort.Strings(filePaths)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("no yml file selected")
	}
	return filePaths, nil
}

type partialTestSuite struct {
	Name string `json:"name" yaml:"name"`
	Vars H      `yaml:"vars" json:"vars"`
}

func (v *Venom) readFiles(ctx context.Context, filesPath []string) (err error) {
	for _, f := range filesPath {
		log.Info("Reading ", f)
		btes, err := ioutil.ReadFile(f)
		if err != nil {
			return errors.Wrapf(err, "unable to read file %q", f)
		}

		varCloned := v.variables.Clone()

		fromPartial, err := getVarFromPartialYML(ctx, btes)
		if err != nil {
			return errors.Wrapf(err, "unable to get vars from file %q", f)
		}

		varsFromPartial, err := DumpStringPreserveCase(fromPartial)
		if err != nil {
			return errors.Wrapf(err, "unable to parse variables")
		}

		// we take default vars from the testsuite, only if it's not already is global vars
		for k, value := range varsFromPartial {
			if _, ok := varCloned[k]; !ok || (varCloned[k] == "{}" && varCloned["__Len__"] == "0") {
				// we interpolate the value of vars here, to do it only once per ts
				valueInterpolated, err := interpolate.Do(value, varsFromPartial)
				if err != nil {
					return errors.Wrapf(err, "unable to parse variable %q", k)
				}
				varCloned[k] = valueInterpolated
			}
		}

		vars, err := DumpStringPreserveCase(varCloned)
		if err != nil {
			return errors.Wrapf(err, "unable to parse variables")
		}

		content, err := interpolate.Do(string(btes), vars)
		if err != nil {
			return err
		}

		var ts TestSuite
		if err := yaml.Unmarshal([]byte(content), &ts); err != nil {
			Error(context.Background(), "file content: %s", content)
			return errors.Wrapf(err, "error while unmarshal file %q", f)
		}

		// Default workdir is testsuite directory
		ts.WorkDir, err = filepath.Abs(filepath.Dir(f))
		if err != nil {
			return errors.Wrapf(err, "Unable to get testsuite's working directory")
		}

		ts.Package = f
		ts.Filename = f
		ts.Vars = varCloned

		ts.Vars.Add("venom.testsuite.workdir", ts.WorkDir)
		ts.Vars.Add("venom.testsuite.shortName", ts.Name)
		ts.Vars.Add("venom.testsuite.filename", ts.Filename)
		ts.Vars.Add("venom.datetime", time.Now().Format(time.RFC3339))
		ts.Vars.Add("venom.timestamp", fmt.Sprintf("%d", time.Now().Unix()))

		nSteps := 0
		for _, tc := range ts.TestCases {
			nSteps += len(tc.testSteps)
			if len(tc.Skipped) >= 1 {
				ts.Skipped += len(tc.Skipped)
			}
		}
		ts.Total = len(ts.TestCases)

		v.testsuites = append(v.testsuites, ts)
	}
	return nil
}
