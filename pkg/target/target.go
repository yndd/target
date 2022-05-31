/*
Copyright 2021 NDD.

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

package target

import (
	"context"
	"fmt"
	"sync"

	gapi "github.com/karimra/gnmic/api"
	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/logging"
	targetv1 "github.com/yndd/target/apis/target/v1"
)

type Target interface {
	// Init initializes the device
	Init(...TargetOption) error
	// WithTarget, initializes the gnmic target
	WithTarget(target *gnmictarget.Target)
	// WithLogging initializes the target logger
	WithLogging(log logging.Logger)
	// Discover, discovers the target and its respective data
	Discover(ctx context.Context) (*targetv1.DiscoveryInfo, error)
	// retrieve the target capabilities
	GNMICap(ctx context.Context) (*gnmi.CapabilityResponse, error)
	// GetConfig, gets the config from the target
	GetConfig(ctx context.Context) (interface{}, error)
	// Get, gets the gnmi path from the tree
	GNMIGet(ctx context.Context, opts ...gapi.GNMIOption) (*gnmi.GetResponse, error)
	// Set creates a single transaction for updates and deletes
	GNMISet(ctx context.Context, updates []*gnmi.Update, deletes []*gnmi.Path) (*gnmi.SetResponse, error)
}

// Targets interface to register target types that implement the Target interface
type TargetRegistry interface {
	// RegisterInitializer registers the vendor type and its respective initialize function
	RegisterInitializer(name targetv1.VendorType, initFn Initializer)
	// Initialize returns a Target which implements the Target interface according the vendor type
	Initialize(name targetv1.VendorType) (Target, error)
}

// NewTargetRegistry create a registery for target vendor types that implement the Target interface
func NewTargetRegistry() TargetRegistry {
	return &targets{
		targets: map[targetv1.VendorType]Initializer{},
	}
}

type targets struct {
	m       sync.RWMutex
	targets map[targetv1.VendorType]Initializer
}

func (t *targets) RegisterInitializer(name targetv1.VendorType, initFn Initializer) {
	t.m.Lock()
	defer t.m.Unlock()
	t.targets[name] = initFn
}

func (t *targets) Initialize(name targetv1.VendorType) (Target, error) {
	t.m.Lock()
	defer t.m.Unlock()
	targetInitializer, ok := t.targets[name]
	if !ok {
		return nil, fmt.Errorf("target not registered: %s\n", name)
	}
	target := targetInitializer()
	return target, nil
}

//var Targets = map[ygotnddtarget.E_NddTarget_VendorType]Initializer{}

type Initializer func() Target

//func Register(name ygotnddtarget.E_NddTarget_VendorType, initFn Initializer) {
//	Targets[name] = initFn
//}

type TargetOption func(Target)

// WithTarget, initializes the gnmic target
func WithTarget(target *gnmictarget.Target) TargetOption {
	return func(t Target) {
		t.WithTarget(target)
	}
}

// WithLogging initializes the target logger
func WithLogging(log logging.Logger) TargetOption {
	return func(t Target) {
		t.WithLogging(log)
	}
}

// GetSupportedModels returns the modeData from the gnmi capabilities
func GetSupportedModels(cap *gnmi.CapabilityResponse) []*gnmi.ModelData {
	return cap.SupportedModels
}

// GetSupportedEncodings retuns the encodings from the gnmi capabilities
func GetSupportedEncodings(cap *gnmi.CapabilityResponse) []string {
	enc := []string{}
	for _, e := range cap.SupportedEncodings {
		enc = append(enc, e.String())
	}
	return enc
}
