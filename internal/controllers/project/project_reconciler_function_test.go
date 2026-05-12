/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package project

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
)

var _ = Describe("Finalizer Management", func() {
	It("should add finalizer on first call", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		added := task.addFinalizer()
		Expect(added).To(BeTrue())
		Expect(project.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should not add finalizer if already present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		added := task.addFinalizer()
		Expect(added).To(BeFalse())
		Expect(project.GetMetadata().GetFinalizers()).To(HaveLen(1))
	})
})

var _ = Describe("Default Values", func() {
	It("should set default status if missing", func() {
		project := privatev1.Project_builder{}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.HasStatus()).To(BeTrue())
		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_PENDING))
	})

	It("should set default state if unspecified", func() {
		project := privatev1.Project_builder{
			Status: privatev1.ProjectStatus_builder{
				State: privatev1.ProjectState_PROJECT_STATE_UNSPECIFIED,
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_PENDING))
	})

	It("should not change existing non-unspecified state", func() {
		project := privatev1.Project_builder{
			Status: privatev1.ProjectStatus_builder{
				State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
	})
})

var _ = Describe("Finalizer Removal", func() {
	It("should remove finalizer when present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller, "other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.removeFinalizer()

		Expect(project.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should not error when finalizer not present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{"other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.removeFinalizer()

		Expect(project.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
	})

	It("should handle missing metadata", func() {
		project := privatev1.Project_builder{}.Build()

		task := &task{
			project: project,
		}

		// Should not panic
		task.removeFinalizer()
	})
})
