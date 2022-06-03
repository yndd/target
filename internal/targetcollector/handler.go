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

package targetcollector

import (
	"fmt"
	"strings"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-yang/pkg/yparser"
	"github.com/yndd/nddp-system/pkg/gvkresource"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/cache/pkg/origin"
	"github.com/yndd/cache/pkg/validator"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	unmanagedResource = "Unmanaged resource"
)

func (c *collector) handleSubscription(resp *gnmi.SubscribeResponse) error {
	log := c.log.WithValues("target", c.gnmicTarget.Config.Name, "address", c.gnmicTarget.Config.Address)
	//log.Debug("handle target update from target")

	switch resp.GetResponse().(type) {
	case *gnmi.SubscribeResponse_Update:
		//log.Debug("handle target update from target", "Prefix", resp.GetUpdate().GetPrefix())

		// check if the target cache exists

		nsTargetName := meta.GetNamespacedName(c.namespace, c.gnmicTarget.Config.Name)
		configCacheNsTargetName := meta.NamespacedName(nsTargetName).GetPrefixNamespacedName(origin.Config)
		systemCacheNsTargetName := meta.NamespacedName(nsTargetName).GetPrefixNamespacedName(origin.System)

		ce, err := c.cache.GetEntry(systemCacheNsTargetName)
		if err != nil {
			log.Debug("handle target update target not found in ygot schema cache")
			return errors.New("target cache does not exist")
		}

		resourceList := ce.GetSystemConfigMap()
		//log.Debug("resourceList", "list", resourceList)

		// check read/write maps

		// lists the k8s resources that should be triggered for a reconcile event since an update happend
		// the the config belonging to the CR
		// this is used to validate if the config is up to date or not
		resourceNames := map[string]struct{}{}
		// handle deletes
		if err := c.handleDeletes(configCacheNsTargetName, resourceNames, resourceList, resp.GetUpdate().Delete); err != nil {
			log.Debug("handleDeletes", "error", err)
			return err
		}

		if err := c.handleUpdates(configCacheNsTargetName, resourceNames, resourceList, resp.GetUpdate().Update); err != nil {
			log.Debug("handleUpdates", "error", err)
			return err
		}
		for resourceName := range resourceNames {
			c.triggerReconcileEvent(resourceName)
		}

	case *gnmi.SubscribeResponse_SyncResponse:
		//log.Debug("SyncResponse")
	}

	return nil
}

// handleDeletes updates the running config to align the target config cache based on the delete information.
// A reconcile event is triggered to the k8s controller if the delete path matches a managed k8s resource (MR)
func (c *collector) handleDeletes(configCacheNsTargetName string, resourceNames map[string]struct{}, resourceList map[string]*ygotnddp.YnddSystem_Gvk, delPaths []*gnmi.Path) error {
	if len(delPaths) > 0 {
		/*
			for _, p := range delPaths {
				c.log.Debug("handleDeletes", "configCacheNsTargetName", configCacheNsTargetName, "path", yparser.GnmiPath2XPath(p, true))
			}
		*/
		ce, err := c.cache.GetEntry(configCacheNsTargetName)
		if err != nil {
			return err
		}

		// validate deletes
		if err := validator.ValidateDelete(ce, delPaths, validator.Origin_Subscription); err != nil {
			return err
		}

		// trigger reconcile event, but group them to avoid multiple reconciliation triggers

		for _, path := range delPaths {
			xpath := yparser.GnmiPath2XPath(path, true)
			resourceName, err := c.findManagedResource(xpath, resourceList)
			if err != nil {
				return err
			}

			if *resourceName != unmanagedResource {
				resourceNames[*resourceName] = struct{}{}
			}
		}
	}
	return nil
}

// handleUpdates updates the running config to align the target config cache based on the update information.
// A reconcile event is triggered to the k8s controller if the update path matches a managed k8s resource (MR)
func (c *collector) handleUpdates(configCacheNsTargetName string, resourceNames map[string]struct{}, resourceList map[string]*ygotnddp.YnddSystem_Gvk, updates []*gnmi.Update) error {
	if len(updates) > 0 {
		/*
			for _, u := range updates {
				c.log.Debug("handleUpdates", "path", yparser.GnmiPath2XPath(u.GetPath(), true), "val", u.GetVal())
			}
		*/

		ce, err := c.cache.GetEntry(configCacheNsTargetName)
		if err != nil {
			return err
		}

		// validate updates
		if err := validator.ValidateUpdate(ce, updates, false, true, validator.Origin_Subscription); err != nil {
			return err
		}

		// check of we need to trigger a reconcile event
		for _, u := range updates {
			xpath := yparser.GnmiPath2XPath(u.GetPath(), true)
			// check if this is a managed resource or unmanged resource
			// name == unmanagedResource is an unmanaged resource
			resourceName, err := c.findManagedResource(xpath, resourceList)
			if err != nil {
				return err
			}
			if *resourceName != unmanagedResource {
				resourceNames[*resourceName] = struct{}{}
			}
		}
	}
	return nil
}

// findManagedResource returns a the k8s resourceName as a string (using gvk convention [group, version, kind, namespace, name])
// by validation the best path match in the resourcelist of the system cache
// if no match is find unmanagedResource is returned, since this path is not managed by the k8s controller
func (c *collector) findManagedResource(xpath string, resourceList map[string]*ygotnddp.YnddSystem_Gvk) (*string, error) {
	matchedResourceName := unmanagedResource
	matchedResourcePath := ""
	for resourceName, r := range resourceList {
		for _, path := range r.Path {
			if strings.Contains(xpath, path) {
				// if there is a better match we use the better match
				if len(path) > len(matchedResourcePath) {
					matchedResourcePath = path
					matchedResourceName = resourceName
				}
			}
		}
	}
	return &matchedResourceName, nil
}

// triggerReconcileEvent triggers an external event to the k8s controller with the object resource reference
// this should result in a reconcile trigger on the k8s controller for the particular cr.
func (c *collector) triggerReconcileEvent(resourceName string) error {
	gvk, err := gvkresource.String2Gvk(resourceName)
	if err != nil {
		return err
	}
	kindgroup := strings.Join([]string{gvk.Kind, gvk.Group}, ".")

	object := getObject(gvk)

	//c.log.Debug("triggerReconcileEvent", "kindgroup", kindgroup, "gvk", gvk, "object", object)

	if eventCh, ok := c.eventChs[kindgroup]; ok {
		c.log.Debug("triggerReconcileEvent with channel lookup", "kindgroup", kindgroup, "gvk", gvk, "object", object)
		eventCh <- event.GenericEvent{
			Object: object,
		}
	}
	return nil
}

// getObject returns the k8s object based on the gvk resource name (group, version, kind, namespace, name)
func getObject(gvk *gvkresource.GVK) client.Object {
	switch gvk.Kind {

	case "SrlConfig":
		//return &srlv1alpha1.SrlConfig{
		//	ObjectMeta: metav1.ObjectMeta{Name: gvk.Name, Namespace: gvk.NameSpace},
		//}
		return nil

	default:
		fmt.Printf("getObject not found gvk: %v\n", *gvk)
		return nil
	}
}
