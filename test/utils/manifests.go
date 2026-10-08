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

package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	envValTrue = "true"
)

// ApplyManifestWithCleanup applies a Kubernetes manifest and registers deferred deletion upon test completion.
func ApplyManifestWithCleanup(path string) {
	ginkgo.GinkgoHelper()

	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", path)
	_, err := Run(cmd)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to apply manifest: "+path)

	ginkgo.DeferCleanup(func() {
		delCmd := exec.Command("kubectl", "delete", "-f", path, "--ignore-not-found")
		_, _ = Run(delCmd)
	})
}

// CreateNamespaceWithCleanup creates a namespace and registers deferred deletion upon test completion.
func CreateNamespaceWithCleanup(ns string) {
	ginkgo.GinkgoHelper()

	ginkgo.By(fmt.Sprintf("creating %s namespace", ns))
	createNsCmd := fmt.Sprintf(
		"kubectl create ns %s --dry-run=client -o yaml | kubectl apply --server-side --force-conflicts -f -",
		ns,
	)
	cmd := exec.Command("sh", "-c", createNsCmd)
	_, err := Run(cmd)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to create namespace")

	ginkgo.DeferCleanup(func() {
		ginkgo.By(fmt.Sprintf("removing %s namespace", ns))
		cmd := exec.Command("kubectl", "delete", "ns", ns)
		_, _ = Run(cmd)

		ginkgo.By(fmt.Sprintf("waiting for %s namespace to be deleted", ns))
		cmd = exec.Command("kubectl", "wait", "--for=delete", "ns/"+ns, "--timeout=2m")
		_, _ = Run(cmd)
	})
}

// ConfigureKubectlKubeRC ensures kubectl kuberc is disabled unless explicitly enabled.
func ConfigureKubectlKubeRC() {
	ginkgo.GinkgoHelper()

	if os.Getenv("KUBECTL_KUBERC") != envValTrue {
		ginkgo.By("disabling kubectl kuberc for test isolation")
		err := os.Setenv("KUBECTL_KUBERC", "false")
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to disable kubectl kuberc")
		_, _ = fmt.Fprintf(ginkgo.GinkgoWriter,
			"kubectl kuberc disabled for consistent test behavior (override with KUBECTL_KUBERC=true)\n")
	} else {
		_, _ = fmt.Fprintf(ginkgo.GinkgoWriter, "kubectl kuberc enabled (KUBECTL_KUBERC=true)\n")
	}
}

// LoadDotEnv loads key-value pairs from .env if present.
func LoadDotEnv() {
	projectDir, err := GetProjectDir()
	if err != nil {
		return
	}
	envPath := projectDir + "/.env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// ManagerImage determines the appropriate manager image to use for e2e tests.
func ManagerImage(defaultImage string) string {
	if img := os.Getenv("IMG"); img != "" {
		return img
	}
	return defaultImage
}
