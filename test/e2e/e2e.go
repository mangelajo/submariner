/*
Copyright 2019 The Kubernetes Authors.

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

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/reporters"
	"github.com/onsi/gomega"

	"github.com/rancher/submariner/test/e2e/framework"
)

func RunE2ETests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	// Register the default reporter, and in addition setup the jUnit XML Reporter
	reporterList := []ginkgo.Reporter{}
	reportDir := framework.TestContext.ReportDir
	if reportDir != "" {
		// Create the directory if it doesn't already exists
		if err := os.MkdirAll(reportDir, 0755); err != nil {
			t.Fatalf("Failed creating jUnit report directory: %v", err)
			return
		}
	}
	// Configure a junit reporter to write to the directory
	junitFile := fmt.Sprintf("junit_%s_%02d.xml",
		framework.TestContext.ReportPrefix,
		config.GinkgoConfig.ParallelNode)
	junitPath := filepath.Join(reportDir, junitFile)
	reporterList = append(reporterList, reporters.NewJUnitReporter(junitPath))
	ginkgo.RunSpecsWithDefaultAndCustomReporters(t, "Submariner E2E suite", reporterList)
}
