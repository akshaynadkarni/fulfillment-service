/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package networkclass

import (
	"context"
	"errors"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

var _ = Describe("buildSpec", func() {
	It("Includes all fields with capabilities", func() {
		title := "UDN Network"
		description := "User-Defined Network implementation"
		implStrategy := "udn-net"

		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Id:                     "nc-test-123",
				Title:                  title,
				Description:            description,
				ImplementationStrategy: implStrategy,
				Constraints:            &privatev1.NetworkClassConstraints{},
				Capabilities: privatev1.NetworkClassCapabilities_builder{
					SupportsIpv4:      true,
					SupportsIpv6:      true,
					SupportsDualStack: true,
				}.Build(),
			}.Build(),
		}

		spec := task.buildSpec()

		Expect(spec["title"]).To(Equal(title))
		Expect(spec["description"]).To(Equal(description))
		Expect(spec["implementation_strategy"]).To(Equal(implStrategy))
		Expect(spec["constraints"]).To(BeAssignableToTypeOf(map[string]any{}))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_ipv4", true))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_ipv6", true))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_dual_stack", true))
	})

	It("Handles IPv4-only capabilities", func() {
		title := "Physical Network"
		description := "Physical network infrastructure"
		implStrategy := "phys-net"

		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Id:                     "nc-test-456",
				Title:                  title,
				Description:            description,
				ImplementationStrategy: implStrategy,
				Capabilities: privatev1.NetworkClassCapabilities_builder{
					SupportsIpv4: true,
				}.Build(),
			}.Build(),
		}

		spec := task.buildSpec()

		Expect(spec["title"]).To(Equal(title))
		Expect(spec["description"]).To(Equal(description))
		Expect(spec["implementation_strategy"]).To(Equal(implStrategy))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_ipv4", true))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_ipv6", false))
		Expect(spec["capabilities"]).To(HaveKeyWithValue("supports_dual_stack", false))
	})

	It("Handles missing constraints field", func() {
		title := "OVN Kubernetes"
		description := "OVN-based Kubernetes networking"
		implStrategy := "ovn-kubernetes"

		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Id:                     "nc-test-789",
				Title:                  title,
				Description:            description,
				ImplementationStrategy: implStrategy,
				Capabilities: privatev1.NetworkClassCapabilities_builder{
					SupportsIpv6: true,
				}.Build(),
			}.Build(),
		}

		spec := task.buildSpec()

		Expect(spec["title"]).To(Equal(title))
		Expect(spec["description"]).To(Equal(description))
		Expect(spec["implementation_strategy"]).To(Equal(implStrategy))
		Expect(spec).ToNot(HaveKey("constraints"))
	})

	It("Handles missing capabilities field", func() {
		title := "Basic Network"
		description := "Basic network class"
		implStrategy := "basic-net"

		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Id:                     "nc-test-no-caps",
				Title:                  title,
				Description:            description,
				ImplementationStrategy: implStrategy,
			}.Build(),
		}

		spec := task.buildSpec()

		Expect(spec["title"]).To(Equal(title))
		Expect(spec["description"]).To(Equal(description))
		Expect(spec["implementation_strategy"]).To(Equal(implStrategy))
		Expect(spec).ToNot(HaveKey("capabilities"))
	})
})

// newNetworkClassCR creates an unstructured NetworkClass CR for use with the fake client.
func newNetworkClassCR(id, namespace, name string, deletionTimestamp *metav1.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvks.NetworkClass)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetLabels(map[string]string{
		labels.NetworkClassUuid: id,
	})
	if deletionTimestamp != nil {
		obj.SetDeletionTimestamp(deletionTimestamp)
		obj.SetFinalizers([]string{"osac.openshift.io/networkclass"})
	}
	return obj
}

// hasFinalizer checks if the fulfillment-controller finalizer is present on the network class.
func hasFinalizer(networkClass *privatev1.NetworkClass) bool {
	return slices.Contains(networkClass.GetMetadata().GetFinalizers(), finalizers.Controller)
}

// newTaskForDelete creates a task configured for testing delete() with hub-dependent paths.
func newTaskForDelete(networkClassID, hubID string, hubCache controllers.HubCache) *task {
	networkClass := privatev1.NetworkClass_builder{
		Id: networkClassID,
		Metadata: privatev1.Metadata_builder{
			Finalizers: []string{finalizers.Controller},
		}.Build(),
		Status: privatev1.NetworkClassStatus_builder{
			Hub: hubID,
		}.Build(),
	}.Build()

	f := &function{
		logger:   logger,
		hubCache: hubCache,
	}

	return &task{
		r:            f,
		networkClass: networkClass,
	}
}

var _ = Describe("delete", func() {
	var (
		ctx            context.Context
		ctrl           *gomock.Controller
		mockHubCache   *controllers.MockHubCache
		fakeClient     clnt.Client
		scheme         *runtime.Scheme
		networkClassID string
		hubID          string
		namespace      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockHubCache = controllers.NewMockHubCache(ctrl)

		networkClassID = "nc-delete-123"
		hubID = "hub-xyz"
		namespace = "test-namespace"

		scheme = runtime.NewScheme()
		gvkList := schema.GroupVersionKind{
			Group:   gvks.NetworkClass.Group,
			Version: gvks.NetworkClass.Version,
			Kind:    gvks.NetworkClassList.Kind,
		}
		scheme.AddKnownTypeWithName(gvks.NetworkClass, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvkList, &unstructured.UnstructuredList{})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("hub is not yet assigned", func() {
		It("removes the finalizer immediately", func() {
			task := newTaskForDelete(networkClassID, "", mockHubCache)
			task.networkClass.GetStatus().SetHub("")

			err := task.delete(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(hasFinalizer(task.networkClass)).To(BeFalse(), "finalizer should be removed")
		})
	})

	When("hub cache fails", func() {
		It("returns error and does not remove finalizer", func() {
			expectedErr := errors.New("hub cache unavailable")
			mockHubCache.EXPECT().Get(gomock.Any(), hubID).Return(nil, expectedErr)

			task := newTaskForDelete(networkClassID, hubID, mockHubCache)

			err := task.delete(ctx)

			Expect(err).To(MatchError(expectedErr))
			Expect(hasFinalizer(task.networkClass)).To(BeTrue(), "finalizer should remain")
		})
	})

	When("K8s object does not exist", func() {
		It("removes the finalizer", func() {
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

			mockHubCache.EXPECT().
				Get(gomock.Any(), hubID).
				Return(&controllers.HubEntry{
					Namespace: namespace,
					Client:    fakeClient,
				}, nil)

			task := newTaskForDelete(networkClassID, hubID, mockHubCache)

			err := task.delete(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(hasFinalizer(task.networkClass)).To(BeFalse(), "finalizer should be removed")
		})
	})

	When("K8s object exists without deletion timestamp", func() {
		It("deletes the K8s object and keeps the finalizer", func() {
			existingCR := newNetworkClassCR(networkClassID, namespace, "nc-123", nil)

			deleteCalled := false
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingCR).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.DeleteOption) error {
						deleteCalled = true
						return nil
					},
				}).
				Build()

			mockHubCache.EXPECT().
				Get(gomock.Any(), hubID).
				Return(&controllers.HubEntry{
					Namespace: namespace,
					Client:    fakeClient,
				}, nil)

			task := newTaskForDelete(networkClassID, hubID, mockHubCache)

			err := task.delete(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(deleteCalled).To(BeTrue(), "Delete should have been called")
			Expect(hasFinalizer(task.networkClass)).To(BeTrue(), "finalizer should remain until K8s object is fully deleted")
		})
	})

	When("K8s object exists with deletion timestamp (finalizers being processed)", func() {
		It("does not remove the finalizer and waits", func() {
			now := metav1.Now()
			existingCR := newNetworkClassCR(networkClassID, namespace, "nc-456", &now)
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingCR).
				Build()

			mockHubCache.EXPECT().
				Get(gomock.Any(), hubID).
				Return(&controllers.HubEntry{
					Namespace: namespace,
					Client:    fakeClient,
				}, nil)

			task := newTaskForDelete(networkClassID, hubID, mockHubCache)

			err := task.delete(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(hasFinalizer(task.networkClass)).To(BeTrue(), "finalizer should remain while K8s finalizers process")
		})
	})

	When("K8s List operation fails", func() {
		It("returns error and does not remove finalizer", func() {
			expectedErr := errors.New("list failed")
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client clnt.WithWatch, list clnt.ObjectList, opts ...clnt.ListOption) error {
						return expectedErr
					},
				}).
				Build()

			mockHubCache.EXPECT().
				Get(gomock.Any(), hubID).
				Return(&controllers.HubEntry{
					Namespace: namespace,
					Client:    fakeClient,
				}, nil)

			task := newTaskForDelete(networkClassID, hubID, mockHubCache)

			err := task.delete(ctx)

			Expect(err).To(MatchError(expectedErr))
			Expect(hasFinalizer(task.networkClass)).To(BeTrue(), "finalizer should remain on error")
		})
	})
})

var _ = Describe("validateTenant", func() {
	It("succeeds when exactly one tenant is assigned", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-abc"},
				}.Build(),
			}.Build(),
		}

		err := task.validateTenant()

		Expect(err).ToNot(HaveOccurred())
	})

	It("fails when no metadata is present", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{}.Build(),
		}

		err := task.validateTenant()

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("fails when no tenants are assigned", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{},
				}.Build(),
			}.Build(),
		}

		err := task.validateTenant()

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("fails when multiple tenants are assigned", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-1", "tenant-2"},
				}.Build(),
			}.Build(),
		}

		err := task.validateTenant()

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})
})

var _ = Describe("setDefaults", func() {
	It("sets status to PENDING when status is missing", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{}.Build(),
		}

		task.setDefaults()

		Expect(task.networkClass.HasStatus()).To(BeTrue())
		Expect(task.networkClass.GetStatus().GetState()).To(Equal(privatev1.NetworkClassState_NETWORK_CLASS_STATE_PENDING))
	})

	It("sets state to PENDING when state is UNSPECIFIED", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Status: privatev1.NetworkClassStatus_builder{
					State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_UNSPECIFIED,
				}.Build(),
			}.Build(),
		}

		task.setDefaults()

		Expect(task.networkClass.GetStatus().GetState()).To(Equal(privatev1.NetworkClassState_NETWORK_CLASS_STATE_PENDING))
	})

	It("does not change state when already set to READY", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Status: privatev1.NetworkClassStatus_builder{
					State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
				}.Build(),
			}.Build(),
		}

		task.setDefaults()

		Expect(task.networkClass.GetStatus().GetState()).To(Equal(privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY))
	})

	It("does not change state when already set to FAILED", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Status: privatev1.NetworkClassStatus_builder{
					State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_FAILED,
				}.Build(),
			}.Build(),
		}

		task.setDefaults()

		Expect(task.networkClass.GetStatus().GetState()).To(Equal(privatev1.NetworkClassState_NETWORK_CLASS_STATE_FAILED))
	})
})

var _ = Describe("addFinalizer", func() {
	It("adds finalizer when not present and creates metadata", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{}.Build(),
		}

		added := task.addFinalizer()

		Expect(added).To(BeTrue())
		Expect(hasFinalizer(task.networkClass)).To(BeTrue())
	})

	It("adds finalizer when not present but metadata exists", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{"other-finalizer"},
				}.Build(),
			}.Build(),
		}

		added := task.addFinalizer()

		Expect(added).To(BeTrue())
		Expect(hasFinalizer(task.networkClass)).To(BeTrue())
		Expect(task.networkClass.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})

	It("does not add finalizer when already present", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{finalizers.Controller},
				}.Build(),
			}.Build(),
		}

		added := task.addFinalizer()

		Expect(added).To(BeFalse())
		finalizerList := task.networkClass.GetMetadata().GetFinalizers()
		Expect(finalizerList).To(HaveLen(1))
		Expect(finalizerList[0]).To(Equal(finalizers.Controller))
	})
})

var _ = Describe("removeFinalizer", func() {
	It("removes the controller finalizer when present", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{finalizers.Controller, "other-finalizer"},
				}.Build(),
			}.Build(),
		}

		task.removeFinalizer()

		Expect(hasFinalizer(task.networkClass)).To(BeFalse())
		Expect(task.networkClass.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
	})

	It("does nothing when finalizer is not present", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{"other-finalizer"},
				}.Build(),
			}.Build(),
		}

		task.removeFinalizer()

		Expect(task.networkClass.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
	})

	It("does nothing when metadata is missing", func() {
		task := &task{
			networkClass: privatev1.NetworkClass_builder{}.Build(),
		}

		task.removeFinalizer()

		Expect(task.networkClass.HasMetadata()).To(BeFalse())
	})
})
