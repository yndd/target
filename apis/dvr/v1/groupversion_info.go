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

// Package v1 contains API Schema definitions for the driver v1 API group
//+kubebuilder:object:generate=true
//+groupName=dvr.ndd.yndd.io
package v1

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// Package type metadata.
const (
	Group   = "dvr.ndd.yndd.io"
	Version = "v1"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: Group, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Target type metadata.
var (
	TargetKind             = reflect.TypeOf(Target{}).Name()
	TargetGroupKind        = schema.GroupKind{Group: Group, Kind: TargetKind}.String()
	TargetKindAPIVersion   = TargetKind + "." + GroupVersion.String()
	TargetGroupVersionKind = GroupVersion.WithKind(TargetKind)
)

// TargetUsage type metadata.
var (
	TargetUsageKind             = reflect.TypeOf(TargetUsage{}).Name()
	TargetUsageGroupKind        = schema.GroupKind{Group: Group, Kind: TargetUsageKind}.String()
	TargetUsageKindAPIVersion   = TargetUsageKind + "." + GroupVersion.String()
	TargetUsageGroupVersionKind = GroupVersion.WithKind(TargetUsageKind)

	TargetUsageListKind             = reflect.TypeOf(TargetUsageList{}).Name()
	TargetUsageListGroupKind        = schema.GroupKind{Group: Group, Kind: TargetUsageListKind}.String()
	TargetUsageListKindAPIVersion   = TargetUsageListKind + "." + GroupVersion.String()
	TargetUsageListGroupVersionKind = GroupVersion.WithKind(TargetUsageListKind)
)
