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
	"context"
	"errors"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// FunctionBuilder contains the data needed to build instances of the reconciler function.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
}

// NewFunction creates a builder that can be used to configure and create reconciler functions.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger that the reconciler will use to write log messages.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC connection that the reconciler will use to communicate with the API server.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// Build uses the data stored in the builder to create and configure a new reconciler function.
func (b *FunctionBuilder) Build() (result *function, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("connection is mandatory")
		return
	}

	result = &function{
		logger:         b.logger,
		projectsClient: privatev1.NewProjectsClient(b.connection),
		maskCalculator: masks.NewCalculator().Build(),
	}
	return
}

// function is the implementation of the reconciler function.
type function struct {
	logger         *slog.Logger
	projectsClient privatev1.ProjectsClient
	maskCalculator *masks.Calculator
}

// Run executes the reconciliation logic for the given project.
func (r *function) Run(ctx context.Context, project *privatev1.Project) error {
	oldProject := proto.Clone(project).(*privatev1.Project)

	task := &task{
		r:       r,
		project: project,
	}

	var err error
	if project.HasMetadata() && project.GetMetadata().HasDeletionTimestamp() {
		err = task.delete(ctx)
	} else {
		err = task.update(ctx)
	}
	if err != nil {
		return err
	}

	updateMask := r.maskCalculator.Calculate(oldProject, project)

	if len(updateMask.GetPaths()) > 0 {
		_, err = r.projectsClient.Update(ctx, privatev1.ProjectsUpdateRequest_builder{
			Object:     project,
			UpdateMask: updateMask,
		}.Build())
	}

	return err
}

// task contains the data needed to reconcile a single project.
type task struct {
	r       *function
	project *privatev1.Project
}

// update performs the reconciliation logic for creating or updating a project.
func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	t.setDefaults()
	return nil
}

// delete performs the deletion cleanup for a project.
func (t *task) delete(ctx context.Context) error {
	t.removeFinalizer()
	return nil
}

// setDefaults sets default values for the project.
func (t *task) setDefaults() {
	if !t.project.HasStatus() {
		t.project.SetStatus(&privatev1.ProjectStatus{})
	}
	if t.project.GetStatus().GetState() == privatev1.ProjectState_PROJECT_STATE_UNSPECIFIED {
		t.project.GetStatus().SetState(privatev1.ProjectState_PROJECT_STATE_PENDING)
	}
}

// addFinalizer adds the controller finalizer to the project if not already present.
// Returns true if the finalizer was added (indicating the update should be saved immediately).
func (t *task) addFinalizer() bool {
	if !t.project.HasMetadata() {
		t.project.SetMetadata(&privatev1.Metadata{})
	}
	list := t.project.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.project.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

// removeFinalizer removes the controller finalizer from the project.
func (t *task) removeFinalizer() {
	if !t.project.HasMetadata() {
		return
	}
	list := t.project.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.project.GetMetadata().SetFinalizers(list)
	}
}
