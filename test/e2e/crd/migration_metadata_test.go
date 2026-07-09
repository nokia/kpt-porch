// Copyright 2026 The kpt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package crd

import (
	porchv1alpha1 "github.com/kptdev/porch/api/porch/v1alpha1"
	porchv1alpha2 "github.com/kptdev/porch/api/porch/v1alpha2"
	configapi "github.com/kptdev/porch/api/porchconfig/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Migration Metadata Preservation", Ordered, Label("migration"), func() {
	// Tests that labels and annotations set on v1alpha1 PackageRevision
	// resources are preserved on the v1alpha2 CRDs after migration.
	//
	// Flow:
	//   1. Register a repo as v1alpha1 (no migration annotation)
	//   2. Create a package via v1alpha1 API with labels and annotations
	//   3. Publish the package
	//   4. Set additional labels/annotations on the published PR
	//   5. Enable v1alpha2 migration annotation
	//   6. Verify the v1alpha2 CRD carries those labels and annotations

	const (
		repoName  = "mig-meta-repo"
		pkgName   = "labeled-pkg"
		workspace = "v1"
	)

	var env *testEnv

	BeforeAll(func() {
		env = sharedEnv()

		By("creating a fresh gitea repo for migration metadata test")
		deleteGiteaRepo(repoName)
		createGiteaRepo(repoName)
	})

	AfterAll(func() {
		cleanupMigrationResources(env.Ctx, env.Namespace, repoName)
		deleteGiteaRepo(repoName)
	})

	It("should preserve labels and annotations during migration", func() {
		By("registering repo as v1alpha1 (no migration annotation)")
		registerV1Alpha1Repo(env.Ctx, env.Namespace, repoName)

		By("creating and publishing a package via v1alpha1 API")
		published := createAndPublishV1Alpha1Package(env.Ctx, env.Namespace, repoName, pkgName, workspace)

		By("setting custom labels and annotations on the v1alpha1 PackageRevision")
		Eventually(func(g Gomega) {
			pr := &porchv1alpha1.PackageRevision{}
			g.Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: published.name}, pr)).To(Succeed())

			if pr.Labels == nil {
				pr.Labels = map[string]string{}
			}
			pr.Labels["team"] = "networking"
			pr.Labels["env"] = "production"

			if pr.Annotations == nil {
				pr.Annotations = map[string]string{}
			}
			pr.Annotations["contact"] = "team-net@example.com"
			pr.Annotations["purpose"] = "infra-base"

			g.Expect(k8sClient.Update(env.Ctx, pr)).To(Succeed())
		}).WithTimeout(defaultTimeout).WithPolling(defaultInterval).Should(Succeed())

		By("verifying labels are persisted on v1alpha1")
		pr := &porchv1alpha1.PackageRevision{}
		Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: published.name}, pr)).To(Succeed())
		Expect(pr.Labels).To(HaveKeyWithValue("team", "networking"))
		Expect(pr.Labels).To(HaveKeyWithValue("env", "production"))
		Expect(pr.Annotations).To(HaveKeyWithValue("contact", "team-net@example.com"))
		Expect(pr.Annotations).To(HaveKeyWithValue("purpose", "infra-base"))

		By("enabling v1alpha2 migration and triggering sync")
		Eventually(func(g Gomega) {
			repo := &configapi.Repository{}
			g.Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: repoName}, repo)).To(Succeed())
			if repo.Annotations == nil {
				repo.Annotations = map[string]string{}
			}
			repo.Annotations["porch.kpt.dev/v1alpha2-migration"] = "true"
			now := metav1.Now()
			if repo.Spec.Sync == nil {
				repo.Spec.Sync = &configapi.RepositorySync{}
			}
			repo.Spec.Sync.RunOnceAt = &now
			g.Expect(k8sClient.Update(env.Ctx, repo)).To(Succeed())
		}).WithTimeout(defaultTimeout).WithPolling(defaultInterval).Should(Succeed())

		By("waiting for v1alpha2 CRD to appear with seeded labels")
		v2Name := crdName(repoName, pkgName, workspace)
		Eventually(func(g Gomega) {
			v2pr := &porchv1alpha2.PackageRevision{}
			g.Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: v2Name}, v2pr)).To(Succeed())
			// Wait for the seed apply to complete — labels appear after the main apply
			g.Expect(v2pr.Labels).To(HaveKeyWithValue("team", "networking"),
				"seed labels not yet applied, current labels: %v", v2pr.Labels)
		}).WithTimeout(defaultTimeout).WithPolling(defaultInterval).Should(Succeed())

		By("verifying source labels are preserved on the v1alpha2 CRD")
		v2pr := &porchv1alpha2.PackageRevision{}
		Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: v2Name}, v2pr)).To(Succeed())

		Expect(v2pr.Labels).To(HaveKeyWithValue("team", "networking"))
		Expect(v2pr.Labels).To(HaveKeyWithValue("env", "production"))

		By("verifying source annotations are preserved on the v1alpha2 CRD")
		Expect(v2pr.Annotations).To(HaveKeyWithValue("contact", "team-net@example.com"))
		Expect(v2pr.Annotations).To(HaveKeyWithValue("purpose", "infra-base"))

		By("verifying system labels are also present (not clobbered)")
		Expect(v2pr.Labels).To(HaveKeyWithValue(porchv1alpha2.RepositoryLabelKey, repoName))
		Expect(v2pr.Labels).To(HaveKey(porchv1alpha2.LatestPackageRevisionKey))
	})

	It("should not carry system-only labels as source metadata", func() {
		// The v1alpha1 PR will also have system labels (latest-revision, repository).
		// These must NOT be double-applied via the seed — they are already managed
		// by the main repo controller field manager.
		v2Name := crdName(repoName, pkgName, workspace)
		v2pr := &porchv1alpha2.PackageRevision{}
		Expect(k8sClient.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace, Name: v2Name}, v2pr)).To(Succeed())

		// The repo label should be the repo name (set by repo controller, not seed)
		Expect(v2pr.Labels[porchv1alpha2.RepositoryLabelKey]).To(Equal(repoName))

		// Count non-system labels — should be exactly our 2 custom ones
		nonSystemCount := 0
		for k := range v2pr.Labels {
			if k != porchv1alpha2.RepositoryLabelKey && k != porchv1alpha2.LatestPackageRevisionKey {
				nonSystemCount++
			}
		}
		Expect(nonSystemCount).To(Equal(2), "only custom source labels should be present")
	})
})
