//go:build e2e_gpu

/*
Copyright 2026 Google LLC

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

package gpu

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/GoogleCloudPlatform/k8s-bridge/test/utils"
)

const (
	namespace             = "k8s-bridge-system"
	slurmNamespace        = utils.DefaultSlurmNamespace
	defaultSlurmPartition = utils.DefaultSlurmPartition
)

var (
	managerImage = utils.ManagerImage("example.com/k8s-bridge:v0.0.1")
)

func TestE2EGPU(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting k8s-bridge GPU e2e test suite\n")
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 45 * time.Minute
	RunSpecs(t, "e2e gpu suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	utils.LoadDotEnv()
	managerImage = utils.ManagerImage("example.com/k8s-bridge:v0.0.1")

	if os.Getenv("INFRA_PROVIDER") != "gke" && os.Getenv("SKIP_KIND_IMAGE_LOAD") != "true" {
		By("building the manager image")
		buildCmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage))
		_, err := utils.Run(buildCmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to build the manager image")

		By("loading the manager image on Kind")
		err = utils.LoadImageToKindClusterWithName(managerImage)
		Expect(err).NotTo(HaveOccurred(), "Failed to load the manager image into Kind")

		utils.PreloadThirdPartyImages()
	}

	utils.ConfigureKubectlKubeRC()

	utils.EnsureCertManager()
	utils.EnsureJobSet()
	utils.EnsureKueue()

	By("installing CRDs")
	installCmd := exec.Command("make", "install")
	_, err := utils.Run(installCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying Kueue resources (ResourceFlavor, ClusterQueue, LocalQueue)")
	kueueCmd := exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/kueue_resources.yaml")
	_, err = utils.Run(kueueCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy Kueue resources")
})

var _ = AfterSuite(func() {
	By("deleting Kueue resources")
	cmd := exec.Command("kubectl", "delete", "-f", "test/e2e/testdata/kueue_resources.yaml", "--ignore-not-found")
	if _, err := utils.Run(cmd); err != nil {
		utils.WarnError(err)
	}

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall", "ignore-not-found=true")
	if _, err := utils.Run(cmd); err != nil {
		utils.WarnError(err)
	}

	utils.EnsureKueueTeardown()
	utils.EnsureJobSetTeardown()
	utils.EnsureCertManagerTeardown()
})
