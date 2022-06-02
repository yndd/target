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

package targetcontroller

import (
	"reflect"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/target/internal/cache"
	"github.com/yndd/target/pkg/origin"
)

func (c *targetControllerImpl) StartTarget(nsTargetName string) {
	log := c.log.WithValues("nsTargetName", nsTargetName)
	log.Debug("start target...")
	// the target we get on the channel has <namespace.target> semantics
	targetName := meta.NamespacedName(nsTargetName).GetName()
	namespace := meta.NamespacedName(nsTargetName).GetNameSpace()

	ti, err := NewTargetInstance(c.ctx, &TiOptions{
		Logger:         c.log,
		Namespace:      namespace,
		NsTargetName:   nsTargetName,
		TargetName:     targetName,
		Cache:          c.cache,
		Client:         c.client,
		EventChs:       c.eventChs,
		TargetRegistry: c.options.TargetRegistry,
		Registrator:    c.registrator,
		VendorType:     c.options.VendorType,
	})
	if err != nil {
		//return err
	}
	c.AddTargetInstance(nsTargetName, ti)

	// initialize the config target cache
	configCacheNsTargetName := meta.NamespacedName(nsTargetName).GetPrefixNamespacedName(origin.Config)
	cce := cache.NewCacheEntry(configCacheNsTargetName)
	cce.SetModel(c.options.TargetModel)
	c.cache.AddEntry(cce)

	// initialize the system target cache
	systemCacheNsTargetName := meta.NamespacedName(nsTargetName).GetPrefixNamespacedName(origin.System)
	sce := cache.NewCacheEntry(systemCacheNsTargetName)
	sce.SetModel(&model.Model{
		ModelData:       []*gnmi.ModelData{},
		StructRootType:  reflect.TypeOf((*ygotnddp.Device)(nil)),
		SchemaTreeRoot:  ygotnddp.SchemaTree["Device"],
		JsonUnmarshaler: ygotnddp.Unmarshal,
		EnumData:        ygotnddp.ΛEnum,
	})
	c.cache.AddEntry(sce)

	if err := ti.GetInitialTargetConfig(); err != nil {
		c.log.Debug("initialize target config", "error", err)
		//return errors.Wrap(err, "cannot get or initialize initial config")
	}

	if err := ti.InitializeSystemConfig(); err != nil {
		c.log.Debug("initialize system config", "error", err)
		//return errors.Wrap(err, "cannot validate system config")
	}

	if err := ti.StartTargetReconciler(); err != nil {
		c.log.Debug("start target reconciler", "error", err)
		//return errors.Wrap(err, "cannot start target reconciler")
	}

	if err := ti.StartTargetCollector(); err != nil {
		c.log.Debug("start target collector", "error", err)
		//return errors.Wrap(err, "cannot start target collector")
	}

	ti.Register()

	//return nil
}

func (c *targetControllerImpl) StopTarget(nsTargetName string) {
	// delete the target instance -> stops the collectors, reconciler
	c.DeleteTargetInstance(nsTargetName)

	//return nil
}
