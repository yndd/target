/*
Copyright 2021 Wim Henderickx.

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

//+kubebuilder:object:generate=true
//+groupName=dvr.ndd.yndd.io
package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
)

// ConditionReasons a package is or is not installed.
const (
	ConditionReasonNotDiscovered      nddv1.ConditionReason = "UndiscoveredTarget"
	ConditionReasonInvalidCredentials nddv1.ConditionReason = "InvalidCredentialsForTarget"
)

// NotDiscovered indicates that the target is not discovered.
func NotDiscovered() nddv1.Condition {
	return nddv1.Condition{
		Kind:               nddv1.ConditionKindReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ConditionReasonNotDiscovered,
	}
}

// InvalidCredentials indicates that the target has invalid credentials.
func InvalidCredentials() nddv1.Condition {
	return nddv1.Condition{
		Kind:               nddv1.ConditionKindReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ConditionReasonInvalidCredentials,
	}
}
