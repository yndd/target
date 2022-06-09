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
package v1

import (
	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
)

/*
var _ TgList = &TargetList{}

// +k8s:deepcopy-gen=false
type TgList interface {
	client.ObjectList

	GetTargets() []Tg
}

func (x *TargetList) GetTargets() []Tg {
	xs := make([]Tg, len(x.Items))
	for i, r := range x.Items {
		r := r // Pin range variable so we can take its address.
		xs[i] = &r
	}
	return xs
}

var _ Tg = &Target{}
// +k8s:deepcopy-gen=false
type Tg interface {
	resource.Object
	resource.Conditioned

	GetControllerReference() nddv1.Reference
	SetControllerReference(c nddv1.Reference)

	GetSpec() (*ygotnddtarget.NddTarget_TargetEntry, error)

	GetDiscoveryInfo() DiscoveryInfo
	SetDiscoveryInfo(dd *DiscoveryInfo)
}
*/

// GetCondition of this Network Node.
func (t *Target) GetCondition(ct nddv1.ConditionKind) nddv1.Condition {
	return t.Spec.Status.GetCondition(ct)
}

// SetConditions of the Network Node.
func (t *Target) SetConditions(c ...nddv1.Condition) {
	t.Spec.Status.SetConditions(c...)
}

// GetControllerReference of the Network Node.
func (t *Target) GetControllerReference() nddv1.Reference {
	return t.Spec.Status.ControllerRef
}

// SetControllerReference of the Network Node.
func (t *Target) SetControllerReference(c nddv1.Reference) {
	t.Spec.Status.ControllerRef = c
}

/*
func (t *Target) GetSpec() (*ygotnddtarget.NddTarget_TargetEntry, error) {
	validatedGoStruct, err := m.NewConfigStruct(t.Spec.Properties.Raw, true)
	if err != nil {
		return nil, err
	}
	targetEntry, ok := validatedGoStruct.(*ygotnddtarget.NddTarget_TargetEntry)
	if !ok {
		return nil, errors.New("wrong object ndd target entry")
	}

	return targetEntry, nil
}
*/

func (t *Target) GetDiscoveryInfo() *DiscoveryInfo {
	return t.Spec.DiscoveryInfo
}

func (t *Target) SetDiscoveryInfo(dd *DiscoveryInfo) {
	t.Spec.DiscoveryInfo = dd

}
