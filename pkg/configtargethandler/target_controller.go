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

package configtargethandler

import (
	"context"
	"reflect"
	"sync"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/yndd/ndd-runtime/pkg/logging"
	"github.com/yndd/ndd-runtime/pkg/meta"
	"github.com/yndd/ndd-runtime/pkg/model"
	"github.com/yndd/ndd-runtime/pkg/resource"
	"github.com/yndd/nddp-system/pkg/ygotnddp"
	"github.com/yndd/registrator/registrator"
	targetv1 "github.com/yndd/target/apis/target/v1"
	"github.com/yndd/target/internal/cache"
	"github.com/yndd/target/pkg/origin"
	"github.com/yndd/target/pkg/target"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// errors
	errCreateGnmiClient     = "cannot create gnmi client"
	errTargetInitFailed     = "cannot initialize the target"
	errTargetiscoveryFailed = "cannot discover target"
)

// ConfigTargetController defines the interfaces for the target configuration controller
type ConfigTargetController interface {
	// start the target instance
	StartTarget(nsTargetName string)
	// stops the target instance
	StopTarget(nsTargetName string)
	// add a target instance to the target configuration controller
	AddTargetInstance(targetName string, t TargetInstance)
	// delete a target instance from the target configuration controller
	DeleteTargetInstance(targetName string) error
	// get a target instance from the target configuration controller
	GetTargetInstance(targetName string) TargetInstance
}

type Options struct {
	Logger         logging.Logger
	Registrator    registrator.Registrator
	TargetRegistry target.TargetRegistry
	TargetModel    *model.Model
	VendorType     targetv1.VendorType
	Cache          cache.Cache
}

// Option can be used to manipulate Collector config.
type Option func(ConfigTargetController)

// configTargetController implements the ConfigTargetController interface
type configTargetController struct {
	options *Options
	m       sync.RWMutex
	targets map[string]TargetInstance

	// kubernetes
	client   resource.ClientApplicator          // used to get the target credentials
	eventChs map[string]chan event.GenericEvent // TODO to change to a generic gnmi subscription mechanism

	ctx context.Context
	log logging.Logger
}

func New(ctx context.Context, config *rest.Config, o *Options, opts ...Option) ConfigTargetController {
	log := o.Logger
	log.Debug("new target controller")

	c := &configTargetController{
		log:     o.Logger,
		options: o, // contains all options
		m:       sync.RWMutex{},
		targets: make(map[string]TargetInstance),
		ctx:     ctx,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *configTargetController) GetTargetInstance(targetName string) TargetInstance {
	c.m.Lock()
	defer c.m.Unlock()
	t, ok := c.targets[targetName]
	if !ok {
		return nil
	}
	return t
}

func (c *configTargetController) AddTargetInstance(nsTargetName string, t TargetInstance) {
	c.m.Lock()
	defer c.m.Unlock()
	c.targets[nsTargetName] = t
}

func (c *configTargetController) DeleteTargetInstance(nsTargetName string) error {
	c.m.Lock()
	defer c.m.Unlock()
	if ti, ok := c.targets[nsTargetName]; ok {
		if err := ti.StopTargetCollector(); err != nil {
			return err
		}
		if err := ti.StopTargetReconciler(); err != nil {
			return err
		}
		ti.DeRegister()
	}
	delete(c.targets, nsTargetName)
	return nil
}

func (c *configTargetController) StartTarget(nsTargetName string) {
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
		Cache:          c.options.Cache,
		Client:         c.client,
		EventChs:       c.eventChs,
		TargetRegistry: c.options.TargetRegistry,
		Registrator:    c.options.Registrator,
		VendorType:     c.options.VendorType,
	})
	if err != nil {
		//return err
		log.Debug("create new target instance failed", "error", err)
	}
	c.AddTargetInstance(nsTargetName, ti)

	// initialize the config target cache
	configCacheNsTargetName := meta.NamespacedName(nsTargetName).GetPrefixNamespacedName(origin.Config)
	cce := cache.NewCacheEntry(configCacheNsTargetName)
	cce.SetModel(c.options.TargetModel)
	c.options.Cache.AddEntry(cce)

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
	c.options.Cache.AddEntry(sce)

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

func (c *configTargetController) StopTarget(nsTargetName string) {
	log := c.log.WithValues("nsTargetName", nsTargetName)
	log.Debug("delete target...")
	// delete the target instance -> stops the collectors, reconciler
	c.DeleteTargetInstance(nsTargetName)
}
