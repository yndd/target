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

	gapi "github.com/karimra/gnmic/api"
	gnmictarget "github.com/karimra/gnmic/target"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/logging"
	targetv1 "github.com/yndd/ndd-target-runtime/apis/dvr/v1"
	"github.com/yndd/ndd-target-runtime/pkg/ygotnddtarget"
)

type Target interface {
	// Init initializes the device
	Init(...TargetOption) error
	// WithTarget, initializes the device target
	WithTarget(target *gnmictarget.Target)
	// WithLogging initializes the device logging
	WithLogging(log logging.Logger)
	// Discover, discovers the device and its respective data
	Discover(ctx context.Context) (*targetv1.DiscoveryInfo, error)
	// retrieve the device capabilities
	GNMICap(ctx context.Context) (*gnmi.CapabilityResponse, error)
	// retrieve device supported models using gNMI capabilities RPC
	//SupportedModels(ctx context.Context) ([]*gnmi.ModelData, error)
	// GetConfig, gets the config from the device
	GetConfig(ctx context.Context) (interface{}, error)
	// Get, gets the gnmi path from the tree
	GNMIGet(ctx context.Context, opts ...gapi.GNMIOption) (*gnmi.GetResponse, error)
	// Set creates a single transaction for updates and deletes
	GNMISet(ctx context.Context, updates []*gnmi.Update, deletes []*gnmi.Path) (*gnmi.SetResponse, error)
}

var Targets = map[ygotnddtarget.E_NddTarget_VendorType]Initializer{}

type Initializer func() Target

func Register(name ygotnddtarget.E_NddTarget_VendorType, initFn Initializer) {
	Targets[name] = initFn
}

type TargetOption func(Target)

func WithTarget(target *gnmictarget.Target) TargetOption {
	return func(t Target) {
		t.WithTarget(target)
	}
}

func WithLogging(log logging.Logger) TargetOption {
	return func(t Target) {
		t.WithLogging(log)
	}
}

func GetSupportedModels(cap *gnmi.CapabilityResponse) []*gnmi.ModelData {
	return cap.SupportedModels
}

func GetSupportedEncodings(cap *gnmi.CapabilityResponse) []string {
	enc := []string{}
	for _, e := range cap.SupportedEncodings {
		enc = append(enc, e.String())
	}
	return enc
}
