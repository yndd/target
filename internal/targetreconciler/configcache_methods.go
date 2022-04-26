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

package targetreconciler

import (
	"encoding/json"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ygot"
	"github.com/yndd/ndd-target-runtime/internal/validator"
	"github.com/yndd/ndd-target-runtime/internal/cachename"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
)

func (r *reconciler) getSpecdata(resource *ygotnddp.NddpSystem_Gvk) (interface{}, error) {
	spec := resource.Spec
	//r.log.Debug("getSpecdata", "specdata", *spec)
	var x1 interface{}
	if err := json.Unmarshal([]byte(*spec), &x1); err != nil {
		r.log.Debug("getSpecdata", "error", err)
		return nil, err
	}
	return x1, nil
}

// validateCreate takes the current config/goStruct and merge it with the newGoStruct
// validate the new GoStruct against the targetSchema and return the newGoStruct
func (r *reconciler) validateCreate(resource *ygotnddp.NddpSystem_Gvk) (ygot.ValidatedGoStruct, error) {
	x, err := r.getSpecdata(resource)
	if err != nil {
		return nil, err
	}

	configCacheNsTargetName := cachename.NamespacedName(r.nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	ce, err := r.cache.GetEntry(configCacheNsTargetName)
	if err != nil {
		return nil, err
	}

	return validator.ValidateCreate(ce, x)
}

// validateDelete deletes the paths from the current config/goStruct and validates the result
func (r *reconciler) validateDelete(paths []*gnmi.Path) error {
	configCacheNsTargetName := cachename.NamespacedName(r.nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	ce, err := r.cache.GetEntry(configCacheNsTargetName)
	if err != nil {
		return err
	}

	return validator.ValidateDelete(ce, paths, validator.Origin_Reconciler)
}

// validateUpdate updates the current config/goStruct and validates the result
func (r *reconciler) validateUpdate(updates []*gnmi.Update, jsonietf bool) error {
	configCacheNsTargetName := cachename.NamespacedName(r.nsTargetName).GetPrefixNamespacedName(cachename.ConfigCachePrefix)
	ce, err := r.cache.GetEntry(configCacheNsTargetName)
	if err != nil {
		return err
	}

	return validator.ValidateUpdate(ce, updates, false, jsonietf, validator.Origin_Reconciler)
}
